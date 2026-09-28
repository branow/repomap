package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/branow/llm"
	"github.com/branow/repomap/internal/cmdutil"
	"github.com/branow/repomap/internal/config"
	"github.com/branow/repomap/internal/credentials"
	"github.com/branow/repomap/internal/index"
	"github.com/branow/repomap/internal/iostreams"
	"github.com/branow/repomap/internal/output"
	"github.com/branow/repomap/internal/plan"
)

// DefaultOut is where the index tree lands when --out is not given: beside the
// project it describes, so the index travels with the checkout that needs it.
const DefaultOut = ".repomap"

// Concurrency bounds how many model calls may be in flight, and how many
// repositories the build describes at once. Every repository is independent and
// the describe stage is the long pole of a run, so this is a target rather than
// only a ceiling. Four is deliberately modest: the cap that matters is the
// provider's rate limit, not this machine.
const Concurrency = 4

// indexOptions is the flag surface of `repomap index`. --force is a persistent
// root flag, so it is read off the factory rather than declared again here.
type indexOptions struct {
	match   string
	limit   int
	out     string
	backend string
	years   int
	dryRun  bool
}

// newIndex builds the command that produces the index. It only resolves and
// reports: the stage order lives in internal/index.
func newIndex(f *cmdutil.Factory) *cobra.Command {
	opts := indexOptions{years: 1}
	cmd := &cobra.Command{
		Use:   "index [workspace]",
		Short: "Build the index for one workspace",
		Long: "Lists an account's repositories, reads each one's working tree, and " +
			"writes a compact TSV tree an agent reads in one pass.\n\n" +
			"Staleness is decided in two tiers. The forge's activity stamp decides " +
			"what to clone and scan, which is cheap; the content fingerprint of " +
			"what came back decides what to describe, which is a model call. A " +
			"repository with a commit a day therefore gets pulled every run and " +
			"described only when its shape actually changed.\n\n" +
			"With no workspace named, the active profile's is used. --match and " +
			"--limit cover only what they selected and MERGE into the tree already " +
			"there, so rows they did not touch keep their descriptions and their " +
			"fingerprints. The persistent --force flag rebuilds everything, " +
			"ignoring both tiers.\n\n" +
			"Progress goes to stderr and the summary to stdout, so -o json pipes " +
			"cleanly while a person watching the terminal sees the work. --quiet " +
			"silences the progress.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runIndex(c.Context(), f, arg(args), &opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.match, "match", "",
		"only repositories whose name contains this substring")
	flags.IntVar(&opts.limit, "limit", 0,
		"scan at most this many repositories; 0 means all")
	flags.StringVar(&opts.out, "out", DefaultOut, "directory the index tree is written under")
	flags.StringVar(&opts.backend, "backend", "", "llm backend to describe with")
	flags.IntVar(&opts.years, "active-years", 1,
		"how recent a commit must be for the active view to keep a repository")
	flags.BoolVar(&opts.dryRun, "dry-run", false,
		"report the plan; clone nothing and make no model call")
	return cmd
}

// arg is the workspace named on the command line, or empty for the profile's.
func arg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// runIndex resolves what the pipeline needs, runs it, and reports what it did.
// A dry run reads the forge listing and stops, which is what makes it safe
// rather than merely quiet.
func runIndex(ctx context.Context, f *cmdutil.Factory, workspace string,
	opts *indexOptions) error {
	workspace, entry, err := resolveWorkspace(f, workspace)
	if err != nil {
		return err
	}

	// Prove the destination is writable before a single clone or model call: an
	// unwritable --out discovered after the describe stage throws away work that
	// was already paid for.
	if err := writable(opts.out); err != nil {
		return err
	}

	secret, err := forgeSecret(f, workspace, entry)
	if err != nil {
		return err
	}
	source, err := openSource(workspace, entry, secret)
	if err != nil {
		return err
	}
	// Belt for the paths that return before the pipeline closes the source.
	defer source.Close()

	cache, err := cacheRoot()
	if err != nil {
		return err
	}
	client, model, spend, err := describer(f, opts, cache)
	if err != nil {
		return err
	}

	summary, err := index.Build(ctx, source, client, index.Options{
		Workspace:   workspace,
		Out:         opts.out,
		Match:       opts.match,
		Limit:       opts.limit,
		ActiveYears: opts.years,
		Model:       model,
		Workers:     Concurrency,
		Force:       f.Flags.Force,
		DryRun:      opts.dryRun,
		Logger:      newProgress(f.IO, f.Flags.Quiet),
	})
	if err != nil {
		return err
	}
	if spend != nil {
		summary.Usage = *spend
	}
	if err := report(f, workspace, opts.out, summary); err != nil {
		return err
	}
	return describeOutcome(summary)
}

// describeOutcome fails a run whose descriptions did not happen. The index is
// still written - a partial index beats none - but a tree whose description
// column is empty is not a success, and reporting one to CI is how a broken
// backend goes unnoticed for a week.
//
// A repository that could not be cloned is a different thing and does not fail
// the run: it is one row missing from a tree that is otherwise correct, and it
// is named in the summary so nobody has to guess which.
func describeOutcome(summary index.Summary) error {
	if summary.DryRun || len(summary.Missing) == 0 {
		return nil
	}
	return &cmdutil.UnavailableError{
		Subject: "the describe stage",
		Err: fmt.Errorf("%d repositories have no description: %s",
			len(summary.Missing), strings.Join(summary.Missing, ", ")),
	}
}

// resolveWorkspace answers which workspace this build reads: the argument
// first, the active profile second.
func resolveWorkspace(f *cmdutil.Factory, named string) (string, config.Workspace, error) {
	if named == "" {
		_, profile, err := f.Config.Active(config.Overrides{Profile: f.Flags.Profile})
		if err != nil {
			// Only a profile that was actually named is a not-found; an empty
			// name means nothing is configured yet, which needs the fuller hint.
			var missing *config.NotFoundError
			if errors.As(err, &missing) && missing.Kind == config.KindProfile &&
				missing.Name != "" {
				return "", config.Workspace{}, err
			}
			return "", config.Workspace{}, &cmdutil.NotConfiguredError{
				What: "a workspace",
				Fix:  "name one, or run repomap profile switch <name>",
			}
		}
		named = profile.Workspace
	}
	entry, err := f.Config.Workspace(named)
	if err != nil {
		return "", config.Workspace{}, err
	}
	// The name is the tree's directory, so it must be a plain name rather than
	// a path. Config validation already refuses a slash in an entry name; this
	// is the second half of the same rule, kept here because this is where the
	// name becomes a path.
	if strings.ContainsAny(named, `/\`) || named == "." || named == ".." {
		return "", config.Workspace{}, &cmdutil.ValidationError{Field: "workspace",
			Value: named, Reason: "must be a plain name"}
	}
	return named, entry, nil
}

// forgeSecret fetches the workspace's access token. An auth mode that carries
// none - a public account, or a directory of checkouts - is asked for nothing.
func forgeSecret(f *cmdutil.Factory, workspace string,
	entry config.Workspace) (credentials.Secret, error) {
	if !config.NeedsToken(entry.Auth) {
		return credentials.Secret{}, nil
	}
	return f.Store.Get(credentials.ForgeKey(workspace))
}

// describer assembles the model client for this build behind the llm module's
// own middleware, and returns the total it meters into. A dry run is the only
// build allowed to run without a client.
//
// The order of the wrappers is the whole point. The meter goes INNERMOST, so it
// sees one call per request that actually reached the provider: retries count,
// because a retried call is billed, and a response replayed from the disk cache
// does not, because it was paid for on an earlier run. Metering outside the
// cache is how a resumed build reports money it did not spend.
func describer(f *cmdutil.Factory, opts *indexOptions,
	cache string) (llm.Client, string, *llm.Usage, error) {
	if opts.dryRun {
		return nil, "", nil, nil
	}
	named, entry, err := resolveBackend(f, opts.backend)
	if err != nil {
		return nil, "", nil, err
	}

	key := credentials.Secret{}
	if config.NeedsAPIKey(entry.Provider) {
		key, err = f.Store.Get(credentials.LLMKey(named))
		if err != nil {
			return nil, "", nil, err
		}
	}

	client, err := newClient(entry, key)
	if err != nil {
		return nil, "", nil, err
	}

	spend := &llm.Usage{}
	client = llm.WithUsage(client, spend)
	client = llm.WithRetry(client, llm.RetryConfig{})
	client = llm.WithCache(client, filepath.Join(cache, "llm"))
	return llm.WithConcurrency(client, Concurrency), entry.Model, spend, nil
}

// resolveBackend answers which backend describes: --backend first, the active
// profile second.
func resolveBackend(f *cmdutil.Factory, named string) (string, config.Backend, error) {
	if named == "" {
		_, profile, err := f.Config.Active(config.Overrides{Profile: f.Flags.Profile})
		if err != nil {
			var missing *config.NotFoundError
			if errors.As(err, &missing) && missing.Kind == config.KindProfile &&
				missing.Name != "" {
				return "", config.Backend{}, err
			}
			return "", config.Backend{}, &cmdutil.NotConfiguredError{
				What: "an llm backend",
				Fix:  "pass --backend, or run repomap profile switch <name>",
			}
		}
		named = profile.Backend
	}
	entry, err := f.Config.Backend(named)
	if err != nil {
		return "", config.Backend{}, err
	}
	return named, entry, nil
}

// cacheRoot is where the response cache lives, deliberately outside the index
// tree: that tree is the document an agent reads.
func cacheRoot() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", &cmdutil.UnavailableError{Subject: "the user cache directory", Err: err}
	}
	return filepath.Join(dir, "repomap"), nil
}

// progress writes a build's stage lines to the error stream, keeping them out
// of the document the output writer produces - so `-o json` still pipes cleanly
// while a person watching the terminal sees the work.
//
// The lock is not decoration: repositories are described concurrently, so this
// is called from several goroutines at once, and an unsynchronised writer
// interleaves half-lines on a terminal and races on a buffer in a test.
type progress struct {
	io    *iostreams.IOStreams
	quiet bool
	mu    *sync.Mutex
}

func newProgress(io *iostreams.IOStreams, quiet bool) progress {
	return progress{io: io, quiet: quiet, mu: &sync.Mutex{}}
}

func (p progress) Info(message string) { p.write(message) }

func (p progress) Warn(message string) { p.write("warning: " + message) }

func (p progress) write(message string) {
	if p.quiet {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = p.io.ErrOut.Write([]byte(message + "\n"))
}

// report renders the summary, marking a dry run as one so its plan cannot be
// mistaken for a completed build.
func report(f *cmdutil.Factory, workspace, out string, summary index.Summary) error {
	record := output.Record{
		{Name: "workspace", Value: workspace},
		{Name: "out", Value: filepath.Join(out, workspace)},
		{Name: "repositories", Value: summary.Repos},
	}
	if summary.DryRun {
		// A dry run reads the listing and stops, so it knows exactly what the
		// activity signal says. It does NOT know how many repositories will be
		// described: that is decided by a fingerprint of content it has not
		// read. Reporting a describe count here would be a guess printed in the
		// same column a real run fills with a fact.
		record = append(record,
			output.Field{Name: "dry-run", Value: true},
			output.Field{Name: "scan", Value: summary.Scanned},
			output.Field{Name: "untouched", Value: summary.Reused},
			output.Field{Name: "skipped", Value: summary.Skipped},
			output.Field{Name: "dropped", Value: summary.Dropped},
			output.Field{Name: "reasons", Value: reasons(summary.Reasons)},
			output.Field{Name: "describe", Value: pending(summary)},
		)
		return f.Writer().Show(record)
	}
	record = append(record,
		output.Field{Name: "scanned", Value: summary.Scanned},
		output.Field{Name: "untouched", Value: summary.Reused},
		output.Field{Name: "skipped", Value: summary.Skipped},
		output.Field{Name: "dropped", Value: summary.Dropped},
		output.Field{Name: "reasons", Value: reasons(summary.Reasons)},
		output.Field{Name: "described", Value: summary.Described},
		output.Field{Name: "unchanged", Value: summary.Unchanged},
		output.Field{Name: "active", Value: summary.Active},
		output.Field{Name: "broken", Value: broken(summary)},
		output.Field{Name: "missing", Value: len(summary.Missing)},
		output.Field{Name: "input tokens", Value: summary.Usage.InputTokens},
		output.Field{Name: "output tokens", Value: summary.Usage.OutputTokens},
		output.Field{Name: "cost", Value: summary.Usage.Cost},
	)
	return f.Writer().Show(record)
}

// pending is what a dry run can honestly say about the describe stage: an
// upper bound, because only a repository that is scanned can be redescribed,
// and no lower bound at all.
func pending(summary index.Summary) string {
	if summary.Scanned == 0 {
		return "none; nothing is being scanned"
	}
	return "at most " + strconv.Itoa(summary.Scanned) +
		", decided by content fingerprint after the scan"
}

// reasons renders the scan tally in the planner's order, so two builds report
// the same thing the same way.
func reasons(counted map[string]int) string {
	var parts []string
	for _, rule := range plan.ScanReasons {
		if n := counted[string(rule.Reason)]; n > 0 {
			parts = append(parts, string(rule.Reason)+" "+strconv.Itoa(n))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// broken names the repositories a run could not read, rather than counting
// them: a count tells nobody which one to go and look at.
func broken(summary index.Summary) string {
	names := summary.Names()
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// writable reports whether the index tree can be written where --out points.
func writable(dir string) error {
	if dir == "" {
		dir = DefaultOut
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &cmdutil.ValidationError{Field: "--out", Value: dir,
			Reason: "cannot be written: " + err.Error()}
	}
	probe, err := os.CreateTemp(dir, ".repomap-write-*")
	if err != nil {
		return &cmdutil.ValidationError{Field: "--out", Value: dir,
			Reason: "cannot be written: " + err.Error()}
	}
	name := probe.Name()
	probe.Close()
	return os.Remove(name)
}
