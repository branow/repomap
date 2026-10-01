// Package index is the build pipeline: manifest, plan, scan, plan again,
// describe, write. It is the only package that knows the order those happen in.
package index

import (
	"context"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/branow/gollm"
	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/describe"
	"github.com/branow/repomap/internal/plan"
	"github.com/branow/repomap/internal/render"
)

// Options is one build.
type Options struct {
	// Workspace names the directory the tree is written under, and the row the
	// roster carries.
	Workspace string
	// Out is the index root; the workspace directory sits inside it.
	Out string
	// Match keeps only repositories whose name contains this substring.
	Match string
	// Limit caps how many repositories this run scans. 0 is no cap.
	Limit int
	// Force ignores both staleness tiers.
	Force bool
	// DryRun reports the plan and sends no request past the manifest.
	DryRun bool
	// Workers is how many repositories are described at once.
	Workers int
	// ActiveYears is the window the active view keeps.
	ActiveYears int
	Model       string
	Logger      Logger
	// Now is injectable so a test's stamps are deterministic.
	Now func() time.Time
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o Options) dir() string { return filepath.Join(o.Out, o.Workspace) }

// Summary is what a build did, and what it cost.
type Summary struct {
	Repos   int
	Scanned int
	Reused  int
	Skipped int
	Dropped int
	// Remaining is what --limit left for a follow-up run.
	Remaining int
	Described int
	Unchanged int
	// Missing counts repositories the model left without a sentence. A run that
	// leaves any is not a success: the whole point of the tree is the column
	// those rows are empty in.
	Missing []string
	// Broken counts repositories whose clone or scan failed. They are reported
	// and skipped, never fatal: one unreadable repository must not discard a
	// run that already paid for two hundred others.
	Broken  map[string]error
	Written int
	Active  int
	Usage   gollm.Usage
	DryRun  bool
	Reasons map[string]int
}

// Build runs one workspace's index end to end.
func Build(ctx context.Context, src Source, client gollm.Client, opts Options) (Summary, error) {
	dir := opts.dir()

	repos, err := src.Repositories(ctx)
	if err != nil {
		return Summary{}, err
	}
	info(opts.Logger, "manifest: "+describe.Plural(len(repos), "repository", "repositories"))

	state, err := render.State(dir)
	if err != nil {
		return Summary{}, err
	}
	if opts.Force {
		state = map[string]catalog.State{}
	}

	// Both narrowings apply to the plan, never to the manifest: Dropped is only
	// knowable from a complete listing.
	scanPlan := plan.Scan(repos, state)
	scanPlan.Match(opts.Match)
	remaining := scanPlan.Limit(opts.Limit)
	summary := Summary{
		Repos:     len(repos),
		Skipped:   len(scanPlan.Skipped),
		Dropped:   len(scanPlan.Dropped),
		Remaining: remaining,
		Reasons:   tally(scanPlan),
		Broken:    map[string]error{},
		DryRun:    opts.DryRun,
	}
	info(opts.Logger, "plan: "+planLine(scanPlan))

	if opts.DryRun {
		// A dry run reports only what it can actually know. It has not scanned
		// anything, so it cannot say how many repositories will be described -
		// that is decided by a fingerprint of content it has not read. Reporting
		// an upper bound is honest; printing a guess in the column a real run
		// fills with a fact is not.
		summary.Scanned = len(scanPlan.Scan)
		summary.Reused = len(scanPlan.Reuse)
		return summary, nil
	}

	// One probe before anything is cloned: an expired key or a dropped VPN is
	// one message up front rather than a failure per repository.
	if err := reachable(ctx, src, scanPlan.Scan); err != nil {
		return summary, err
	}

	scanned := make([]catalog.Entry, 0, len(scanPlan.Scan))
	for i, repo := range scanPlan.Scan {
		info(opts.Logger, "scan: "+position(i, len(scanPlan.Scan))+" "+repo.Name)
		tree, err := src.Scan(ctx, repo)
		if err != nil {
			summary.Broken[repo.Name] = err
			warn(opts.Logger, "scan: "+repo.Name+" skipped: "+err.Error())
			continue
		}
		scanned = append(scanned, catalog.Entry{Repo: repo, Tree: tree})
	}
	summary.Scanned = len(scanned)
	summary.Reused = len(scanPlan.Reuse)

	// The source is released before the first model call, so a describe run
	// never holds a clone lock or a connection across minutes of third-party
	// latency.
	if err := src.Close(); err != nil {
		return summary, err
	}

	describePlan := plan.Describe(scanned, state)
	summary.Unchanged = len(describePlan.Unchanged)
	info(opts.Logger, "fingerprint: "+describe.Plural(len(describePlan.Describe), "repository", "repositories")+
		" changed, "+describe.Plural(len(describePlan.Unchanged), "repository", "repositories")+" unchanged")

	result, err := describe.Run(ctx, client, describePlan.Describe, describe.Options{
		Model:   opts.Model,
		Workers: opts.Workers,
		Logger:  loggerFor(opts.Logger),
	})
	if err != nil {
		return summary, err
	}
	summary.Usage = result.Usage
	summary.Missing = result.Missing
	for name, err := range result.Failed {
		summary.Broken[name] = err
	}

	written := describePlan.Unchanged
	for _, entry := range describePlan.Describe {
		answer, ok := result.Answers[entry.Key()]
		if !ok {
			// A repository the model skipped is still written, with its
			// fingerprint, so the row exists - and the Undescribed scan rule
			// picks it up next run rather than letting the signal call it
			// untouched and leave it empty forever.
			written = append(written, entry)
			continue
		}
		written = append(written, describe.Apply(entry, answer))
		summary.Described++
	}
	written = append(written, reused(scanPlan.Reuse, state)...)
	written = append(written, skipped(scanPlan.Skipped, state)...)

	if err := writeTree(dir, written, scanPlan.Dropped); err != nil {
		return summary, err
	}
	summary.Written = len(written)

	active, total, err := render.Active(dir, opts.ActiveYears, opts.now())
	if err != nil {
		return summary, err
	}
	summary.Active = active
	info(opts.Logger, "write: "+describe.Plural(total, "row", "rows")+" in "+render.CatalogFile+
		", "+describe.Plural(active, "row", "rows")+" in "+render.ActiveFile)

	if err := render.WriteGuide(opts.Out); err != nil {
		return summary, err
	}
	return summary, render.WriteRoster(opts.Out, render.Roster{
		Workspace: opts.Workspace,
		Forge:     src.Forge(),
		Slug:      src.Slug(),
		Repos:     total,
		Active:    active,
		Files:     countFiles(written),
		Built:     opts.now(),
	})
}

func writeTree(dir string, entries []catalog.Entry, dropped []string) error {
	filesDir := filepath.Join(dir, render.FilesDir)
	for _, entry := range entries {
		// A repository this run did not scan has no inventory to write, and
		// rewriting one from a row would truncate the file that is already
		// correct.
		if len(entry.Tree.Files) == 0 {
			continue
		}
		if err := render.Files(filesDir, entry); err != nil {
			return err
		}
	}
	if _, err := render.Catalog(dir, entries, dropped); err != nil {
		return err
	}
	return render.Prune(filesDir, dropped)
}

// reused carries the previous build's row forward untouched for a repository
// the signal proved unchanged, so a merge writes what was there rather than a
// blank row.
func reused(repos []catalog.Repo, state map[string]catalog.State) []catalog.Entry {
	out := make([]catalog.Entry, 0, len(repos))
	for _, repo := range repos {
		prior, ok := state[repo.Key()]
		if !ok {
			continue
		}
		out = append(out, catalog.Entry{
			Repo:        repo,
			Fingerprint: prior.Fingerprint,
			Description: prior.Description,
		})
	}
	return out
}

// skipped writes a row for a repository that is listed and never described,
// naming the rule that excused it.
func skipped(repos []catalog.Repo, state map[string]catalog.State) []catalog.Entry {
	out := make([]catalog.Entry, 0, len(repos))
	for _, repo := range repos {
		reason, _ := repo.Skipped()
		entry := catalog.Entry{Repo: repo, Skip: reason}
		if prior, ok := state[repo.Key()]; ok {
			entry.Fingerprint = prior.Fingerprint
		}
		out = append(out, entry)
	}
	return out
}

func countFiles(entries []catalog.Entry) int {
	total := 0
	for _, entry := range entries {
		total += len(entry.Tree.Files)
	}
	return total
}

// reachable probes one remote rather than all of them: the failure it exists to
// catch - no network, no VPN, a dead credential - fails every repository
// identically, and probing each would turn one message into two hundred.
func reachable(ctx context.Context, src Source, repos []catalog.Repo) error {
	for _, repo := range repos {
		if repo.Remote != "" {
			return src.Reachable(ctx, repo)
		}
	}
	return nil
}

func tally(p plan.ScanPlan) map[string]int {
	counts := map[string]int{}
	for _, reason := range p.Reasons {
		counts[string(reason)]++
	}
	return counts
}

func planLine(p plan.ScanPlan) string {
	parts := []string{
		describe.Plural(len(p.Scan), "repository", "repositories") + " to scan",
		describe.Plural(len(p.Reuse), "repository", "repositories") + " the activity signal proves untouched",
	}
	if len(p.Skipped) > 0 {
		parts = append(parts, describe.Plural(len(p.Skipped), "repository", "repositories")+" skipped")
	}
	if len(p.Dropped) > 0 {
		parts = append(parts, describe.Plural(len(p.Dropped), "repository", "repositories")+" gone")
	}
	return strings.Join(parts, ", ")
}

func position(i, total int) string {
	return strconv.Itoa(i+1) + "/" + strconv.Itoa(total)
}

// Names lists a summary's broken repositories in stable order.
func (s Summary) Names() []string {
	names := make([]string, 0, len(s.Broken))
	for name := range s.Broken {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
