package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/cmdutil"
	"github.com/branow/repomap/internal/config"
	"github.com/branow/repomap/internal/credentials"
	"github.com/branow/repomap/internal/git"
	"github.com/branow/repomap/internal/index"
	"github.com/branow/repomap/internal/output"
)

// The three outcomes a check reports. They are output, so they are strings.
const (
	statusPass    = "pass"
	statusFail    = "fail"
	statusSkipped = "skipped"
)

// opener turns a workspace record into a build source. It is a seam so every
// check is testable with no forge and no network.
type opener func(name string, entry config.Workspace,
	secret credentials.Secret) (index.Source, error)

// newDoctor builds the preflight command.
func newDoctor(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [workspace]",
		Short: "Check a workspace before a build depends on it",
		Long: "Reports one pass or fail line per check:\n\n" + proves() + "\n" +
			"It is cheap on purpose: one listing call and one probe of one clone " +
			"url. Nothing is cloned, no file is read and no model is called.\n\n" +
			"The two failures worth separating are the ones that look alike from " +
			"a terminal: a token the forge refused, which no amount of waiting " +
			"fixes, and a remote this machine cannot reach, which is usually a " +
			"VPN that dropped.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return runDoctor(c.Context(), f, arg(args), dial)
		},
	}
}

// proves renders the check table into the command's help, so what doctor
// documents and what it runs are one list.
func proves() string {
	var out strings.Builder
	for _, c := range preflights {
		fmt.Fprintf(&out, "  %-11s %s\n", c.Name, c.Doc)
	}
	return out.String()
}

// preflight is one doctor run: what it checks, and what it has opened so far.
type preflight struct {
	factory *cmdutil.Factory
	name    string
	entry   config.Workspace
	open    opener
	secret  credentials.Secret
	source  index.Source
	repos   []catalog.Repo
}

// check is one row of the preflight table: what it proves, and how. The checks
// are data walked in order, so adding one is a row rather than a branch.
type check struct {
	Name string
	// Doc is what this check proves, for the command's own help.
	Doc string
	// Run returns the detail line for a pass, or the error that failed it.
	Run func(context.Context, *preflight) (string, error)
}

// preflights is the check list, in the order each becomes answerable. Each
// depends on the one before, so a failure skips the rest rather than producing
// four cascading complaints about one cause.
var preflights = []check{
	{Name: "git", Doc: "the git binary is installed and runnable", Run: gitCheck},
	{Name: "credential", Doc: "the token this workspace needs is reachable", Run: credential},
	{Name: "forge", Doc: "the workspace resolves into a usable source", Run: forgeCheck},
	{Name: "listing", Doc: "the account answered with its repositories", Run: listing},
	{Name: "clone", Doc: "this machine can reach the repositories' remote", Run: cloneCheck},
	{Name: "backend", Doc: "a model backend is configured to describe with", Run: backendCheck},
}

// gitCheck runs first because every later stage shells out to git, and a
// missing git otherwise surfaces as a clone failure that reads like an access
// problem.
func gitCheck(ctx context.Context, _ *preflight) (string, error) {
	if err := (git.CLI{}).Available(ctx); err != nil {
		return "", err
	}
	return "installed and runnable", nil
}

// credential proves the token exists before anything is dialled. An auth mode
// that carries none is not a gap: a public account and a local directory both
// read without one.
func credential(_ context.Context, p *preflight) (string, error) {
	if !config.NeedsToken(p.entry.Auth) {
		return string(p.entry.Auth) + " carries no stored token", nil
	}
	key := credentials.ForgeKey(p.name)
	secret, err := p.factory.Store.Get(key)
	if err != nil {
		return "", err
	}
	p.secret = secret
	return "found under " + key, nil
}

// forgeCheck resolves the record into a source without any network call.
func forgeCheck(_ context.Context, p *preflight) (string, error) {
	source, err := p.open(p.name, p.entry, p.secret)
	if err != nil {
		return "", err
	}
	p.source = source
	return string(p.entry.Forge) + " " + account(p.entry) + " via " + string(p.entry.Auth), nil
}

// listing is the cheapest real call there is: ask what repositories exist.
func listing(ctx context.Context, p *preflight) (string, error) {
	repos, err := p.source.Repositories(ctx)
	if err != nil {
		return "", err
	}
	p.repos = repos
	if len(repos) == 0 {
		return "the account answered, and holds no repositories", nil
	}
	return strconv.Itoa(len(repos)) + " repositories listed", nil
}

// cloneCheck probes one remote rather than cloning it. The failure it exists to
// catch - no network, no VPN, a dead SSH key - fails every repository
// identically, so one probe answers for all of them.
func cloneCheck(ctx context.Context, p *preflight) (string, error) {
	for _, repo := range p.repos {
		if repo.Remote == "" {
			continue
		}
		if err := p.source.Reachable(ctx, repo); err != nil {
			return "", err
		}
		return "git reached " + repo.Remote, nil
	}
	return "nothing to clone: these working trees are already on this machine", nil
}

// backendCheck resolves the backend without calling it. A model call here would
// cost money to answer a question the config already answers, and `backend add`
// has already made a real call once.
func backendCheck(_ context.Context, p *preflight) (string, error) {
	named, entry, err := resolveBackend(p.factory, "")
	if err != nil {
		return "", err
	}
	if config.NeedsAPIKey(entry.Provider) {
		if _, err := p.factory.Store.Get(credentials.LLMKey(named)); err != nil {
			return "", err
		}
	}
	return named + ": " + string(entry.Provider) + " " + entry.Model, nil
}

// runDoctor walks the checks and reports every one. A failure stops the run,
// but the remaining rows still print as skipped, so the output keeps one shape
// and a reader can see how far it got.
func runDoctor(ctx context.Context, f *cmdutil.Factory, named string, open opener) error {
	name, entry, err := resolveWorkspace(f, named)
	if err != nil {
		return err
	}
	state := &preflight{factory: f, name: name, entry: entry, open: open}
	defer state.close()

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var failure error
	records := make([]output.Record, 0, len(preflights))
	for _, c := range preflights {
		if failure != nil {
			records = append(records, row(c, statusSkipped, "not reached"))
			continue
		}
		detail, err := c.Run(ctx, state)
		if err != nil {
			failure = err
			records = append(records, row(c, statusFail, err.Error()))
			continue
		}
		records = append(records, row(c, statusPass, detail))
	}

	if err := f.Writer().List(records); err != nil {
		return err
	}
	return failure
}

func row(c check, status, detail string) output.Record {
	return output.Record{
		{Name: "check", Value: c.Name},
		{Name: "status", Value: status},
		{Name: "detail", Value: detail},
	}
}

// close releases whatever the run opened.
func (p *preflight) close() {
	if p.source != nil {
		_ = p.source.Close()
	}
}

// dial is the real opener.
func dial(name string, entry config.Workspace,
	secret credentials.Secret) (index.Source, error) {
	return openSource(name, entry, secret)
}
