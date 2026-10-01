package index

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/branow/gollm"
	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/describe"
	"github.com/branow/repomap/internal/render"
)

// fakeSource is a forge and a clone store with nothing behind them. No test in
// this package opens a socket, runs git, or calls a model.
type fakeSource struct {
	repos   []catalog.Repo
	trees   map[string]catalog.Tree
	broken  map[string]error
	scans   []string
	closed  bool
	reached int
	mu      sync.Mutex
}

func (f *fakeSource) Forge() string { return "fake" }
func (f *fakeSource) Slug() string  { return "acme" }

func (f *fakeSource) Repositories(context.Context) ([]catalog.Repo, error) {
	return f.repos, nil
}

func (f *fakeSource) Reachable(context.Context, catalog.Repo) error {
	f.reached++
	return nil
}

func (f *fakeSource) Scan(_ context.Context, repo catalog.Repo) (catalog.Tree, error) {
	f.mu.Lock()
	f.scans = append(f.scans, repo.Name)
	f.mu.Unlock()
	if err := f.broken[repo.Name]; err != nil {
		return catalog.Tree{}, err
	}
	return f.trees[repo.Name], nil
}

func (f *fakeSource) Close() error {
	f.closed = true
	return nil
}

// fakeClient answers every prompt with one sentence and counts the calls.
type fakeClient struct {
	mu     sync.Mutex
	calls  int
	closed func() bool
	fail   map[string]bool
}

func (c *fakeClient) Name() string { return "fake" }

func (c *fakeClient) Complete(_ context.Context, request gollm.Request) (*gollm.Response, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()

	name := repoOf(request.Prompt)
	if c.fail[name] {
		return nil, errors.New("the provider declined")
	}
	if c.closed != nil && !c.closed() {
		return nil, errors.New("the source was still open when the first model call was made")
	}
	answer := describe.Answer{
		Sentence: "what " + name + " is.",
		Files:    []describe.FileAnswer{{Path: "main.go", Purpose: "process entry point"}},
	}
	body, _ := json.Marshal(answer)
	return &gollm.Response{Structured: body, Usage: gollm.Usage{InputTokens: 10}}, nil
}

func repoOf(prompt string) string {
	line, _, _ := strings.Cut(prompt, "\n")
	return strings.TrimPrefix(line, "Repository: ")
}

func tree(paths ...string) catalog.Tree {
	t := catalog.Tree{Head: "abc123", Branch: "main", LastCommit: "2026-09-01"}
	for _, path := range paths {
		t.Files = append(t.Files, catalog.File{Path: path, Lines: 10, Bytes: 1024})
		t.Bytes += 1024
	}
	return t
}

func newSource() *fakeSource {
	return &fakeSource{
		repos: []catalog.Repo{
			{Name: "api", Activity: "2026-09-01", Remote: "git@example.internal:acme/api.git"},
			{Name: "web", Activity: "2026-09-01", Remote: "git@example.internal:acme/web.git"},
		},
		trees: map[string]catalog.Tree{
			"api": tree("main.go", "internal/router.go"),
			"web": tree("main.go", "app.tsx"),
		},
		broken: map[string]error{},
	}
}

func options(out string) Options {
	return Options{
		Workspace: "acme",
		Out:       out,
		Model:     "test-model",
		Workers:   2,
		Now:       func() time.Time { return time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC) },
	}
}

func TestAFirstBuildWritesTheWholeTree(t *testing.T) {
	out := t.TempDir()
	src, client := newSource(), &fakeClient{}

	summary, err := Build(context.Background(), src, client, options(out))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Described != 2 || client.calls != 2 {
		t.Errorf("described %d with %d calls, want 2 and 2", summary.Described, client.calls)
	}

	for _, path := range []string{
		filepath.Join(out, render.RosterFile),
		filepath.Join(out, render.GuideFile),
		filepath.Join(out, "acme", render.CatalogFile),
		filepath.Join(out, "acme", render.ActiveFile),
		filepath.Join(out, "acme", render.FilesDir, "api.tsv"),
		filepath.Join(out, "acme", render.FilesDir, "web.tsv"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not written", path)
		}
	}
	// The reader is not the builder, so the tree explains itself.
	roster, err := render.ReadRoster(out)
	if err != nil || len(roster) != 1 || roster[0].Repos != 2 {
		t.Errorf("roster = %+v, err = %v", roster, err)
	}
}

// TestASecondRunOverUnchangedInputMakesNoModelCall is the property the whole
// two-tier design exists for. Without it, every repository with a commit a day
// buys a description every run.
func TestASecondRunOverUnchangedInputMakesNoModelCall(t *testing.T) {
	out := t.TempDir()
	client := &fakeClient{}

	if _, err := Build(context.Background(), newSource(), client, options(out)); err != nil {
		t.Fatal(err)
	}
	first := client.calls

	// The forge reports the same activity, so nothing is even scanned.
	summary, err := Build(context.Background(), newSource(), client, options(out))
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != first {
		t.Errorf("a second run made %d more model calls", client.calls-first)
	}
	if summary.Reused != 2 || summary.Scanned != 0 {
		t.Errorf("summary = %+v, want everything proved untouched", summary)
	}
}

// TestAMovedSignalRescansAndTheFingerprintStillDecides: this is the second tier
// doing its job - the repository is pulled, and described only if its shape
// actually changed.
func TestAMovedSignalRescansAndTheFingerprintStillDecides(t *testing.T) {
	out := t.TempDir()
	client := &fakeClient{}
	if _, err := Build(context.Background(), newSource(), client, options(out)); err != nil {
		t.Fatal(err)
	}
	before := client.calls

	// A commit landed in both, changing no file the index renders.
	moved := newSource()
	for i := range moved.repos {
		moved.repos[i].Activity = "2026-09-27"
	}
	summary, err := Build(context.Background(), moved, client, options(out))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 2 {
		t.Errorf("scanned %d, want both rescanned", summary.Scanned)
	}
	if client.calls != before {
		t.Errorf("a commit that changed no file bought %d model calls", client.calls-before)
	}
	if summary.Unchanged != 2 {
		t.Errorf("unchanged = %d, want 2", summary.Unchanged)
	}

	// Now a file appears, and only that repository is described.
	changed := newSource()
	for i := range changed.repos {
		changed.repos[i].Activity = "2026-09-28"
	}
	changed.trees["api"] = tree("main.go", "internal/router.go", "internal/store.go")
	if _, err := Build(context.Background(), changed, client, options(out)); err != nil {
		t.Fatal(err)
	}
	if client.calls != before+1 {
		t.Errorf("made %d calls for one changed repository", client.calls-before)
	}
}

// TestANarrowedRunDoesNotCostTheNextFullOne: --match covers only what it
// selected and merges, so rows it never looked at keep their fingerprints.
func TestANarrowedRunDoesNotCostTheNextFullOne(t *testing.T) {
	out := t.TempDir()
	client := &fakeClient{}
	if _, err := Build(context.Background(), newSource(), client, options(out)); err != nil {
		t.Fatal(err)
	}
	before := client.calls

	// Both repositories changed, but the run is narrowed to one. Deliberately
	// WITHOUT Force: force empties the prior state, which is what masked a bug
	// where a narrowed run deleted every row it did not select.
	changed := newSource()
	for i := range changed.repos {
		changed.repos[i].Activity = "2026-09-28"
	}
	changed.trees["api"] = tree("main.go", "internal/router.go", "internal/store.go")
	changed.trees["web"] = tree("main.go", "app.tsx", "theme.css")

	narrowed := options(out)
	narrowed.Match = "api"
	if _, err := Build(context.Background(), changed, client, narrowed); err != nil {
		t.Fatal(err)
	}
	if client.calls != before+1 {
		t.Errorf("a narrowed run made %d calls, want 1", client.calls-before)
	}
	state, err := render.State(filepath.Join(out, "acme"))
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 2 {
		t.Fatalf("a --match run left %d rows, want both", len(state))
	}
	if state["web"].Description == "" {
		t.Error("the row it did not select lost its description")
	}

	// web was never scanned, so its fingerprint is still the one from the first
	// build - and the next full run pays for web alone, not for api again.
	full, err := Build(context.Background(), changed, client, options(out))
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != before+2 {
		t.Errorf("the next full run made %d calls, want 1 (web only)", client.calls-before-1)
	}
	// api's row already carries the new activity stamp, so the cheap tier proves
	// it untouched and it is never even scanned.
	if full.Reused != 1 {
		t.Errorf("reused = %d, want api proved untouched by the activity signal", full.Reused)
	}
}

// TestAnUnreadableRepositoryIsSkippedNotFatal: one repository nobody can clone
// must not discard a run that already paid for the others.
func TestAnUnreadableRepositoryIsSkippedNotFatal(t *testing.T) {
	out := t.TempDir()
	src := newSource()
	src.broken["web"] = errors.New("Permission denied (publickey).")

	summary, err := Build(context.Background(), src, &fakeClient{}, options(out))
	if err != nil {
		t.Fatalf("one broken repository failed the whole run: %v", err)
	}
	if summary.Described != 1 {
		t.Errorf("described %d, want the readable one", summary.Described)
	}
	if names := summary.Names(); len(names) != 1 || names[0] != "web" {
		t.Errorf("broken = %v, want [web] named so somebody can go and look", names)
	}
}

// TestAFailedDescribeStillWritesTheRow, so the Undescribed rule picks it up next
// run rather than the signal calling it untouched and leaving it empty forever.
func TestAFailedDescribeStillWritesTheRow(t *testing.T) {
	out := t.TempDir()
	client := &fakeClient{fail: map[string]bool{"web": true}}

	summary, err := Build(context.Background(), newSource(), client, options(out))
	if err != nil {
		t.Fatal(err)
	}
	if _, failed := summary.Broken["web"]; !failed {
		t.Error("the failed describe was not reported")
	}

	state, err := render.State(filepath.Join(out, "acme"))
	if err != nil {
		t.Fatal(err)
	}
	if _, written := state["web"]; !written {
		t.Fatal("the row was not written at all")
	}

	// The next run repairs it rather than calling it untouched.
	repaired, err := Build(context.Background(), newSource(), &fakeClient{}, options(out))
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Described != 1 {
		t.Errorf("described %d on the repair run, want the blank row", repaired.Described)
	}
}

// TestTheSourceIsClosedBeforeTheFirstModelCall: describing takes minutes, and
// holding a network source open across it is how a build keeps a connection
// nobody is using.
func TestTheSourceIsClosedBeforeTheFirstModelCall(t *testing.T) {
	out := t.TempDir()
	src := newSource()
	client := &fakeClient{closed: func() bool { return src.closed }}

	if _, err := Build(context.Background(), src, client, options(out)); err != nil {
		t.Fatalf("the source was still open at the first model call: %v", err)
	}
}

// TestADryRunReadsTheListingAndStops.
func TestADryRunReadsTheListingAndStops(t *testing.T) {
	out := t.TempDir()
	src, client := newSource(), &fakeClient{}
	opts := options(out)
	opts.DryRun = true

	summary, err := Build(context.Background(), src, client, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !summary.DryRun || summary.Scanned != 2 {
		t.Errorf("summary = %+v", summary)
	}
	if len(src.scans) != 0 {
		t.Errorf("a dry run scanned %v", src.scans)
	}
	if client.calls != 0 {
		t.Errorf("a dry run made %d model calls", client.calls)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Error("a dry run wrote to the index tree")
	}
}

// TestAnArchivedRepositoryIsListedAndNeverDescribed.
func TestAnArchivedRepositoryIsListedAndNeverDescribed(t *testing.T) {
	out := t.TempDir()
	src := newSource()
	src.repos = append(src.repos, catalog.Repo{Name: "old", Archived: true,
		Activity: "2019-01-01", Remote: "git@example.internal:acme/old.git"})
	client := &fakeClient{}

	summary, err := Build(context.Background(), src, client, options(out))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Skipped != 1 || client.calls != 2 {
		t.Errorf("skipped = %d with %d calls, want 1 and 2", summary.Skipped, client.calls)
	}
	body, err := os.ReadFile(filepath.Join(out, "acme", render.CatalogFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "old\t") {
		t.Error("the archived repository was dropped from the catalog rather than kept")
	}
	if !strings.Contains(string(body), "(archived)") {
		t.Error("the row does not say why it has no description")
	}
}

// TestARepositoryTheForgeNoLongerHasIsRemoved, inventory and all.
func TestARepositoryTheForgeNoLongerHasIsRemoved(t *testing.T) {
	out := t.TempDir()
	if _, err := Build(context.Background(), newSource(), &fakeClient{}, options(out)); err != nil {
		t.Fatal(err)
	}

	shrunk := newSource()
	shrunk.repos = shrunk.repos[:1]
	summary, err := Build(context.Background(), shrunk, &fakeClient{}, options(out))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Dropped != 1 {
		t.Errorf("dropped = %d, want 1", summary.Dropped)
	}
	if _, err := os.Stat(filepath.Join(out, "acme", render.FilesDir, "web.tsv")); !os.IsNotExist(err) {
		t.Error("the inventory of a removed repository survived")
	}
}

// TestAccessIsProbedOnceNotPerRepository.
func TestAccessIsProbedOnceNotPerRepository(t *testing.T) {
	out := t.TempDir()
	src := newSource()
	if _, err := Build(context.Background(), src, &fakeClient{}, options(out)); err != nil {
		t.Fatal(err)
	}
	if src.reached != 1 {
		t.Errorf("probed access %d times, want once for the whole run", src.reached)
	}
}

// TestANarrowedRunNeverDropsRows pins the bug directly: "gone from the forge" is
// only knowable from a complete listing, so a narrowed run must report nothing
// dropped however little it selected.
func TestANarrowedRunNeverDropsRows(t *testing.T) {
	out := t.TempDir()
	if _, err := Build(context.Background(), newSource(), &fakeClient{}, options(out)); err != nil {
		t.Fatal(err)
	}

	for _, match := range []string{"api", "web", "nothing-matches-this"} {
		opts := options(out)
		opts.Match = match
		summary, err := Build(context.Background(), newSource(), &fakeClient{}, opts)
		if err != nil {
			t.Fatal(err)
		}
		if summary.Dropped != 0 {
			t.Errorf("--match %q reported %d dropped", match, summary.Dropped)
		}
		state, err := render.State(filepath.Join(out, "acme"))
		if err != nil {
			t.Fatal(err)
		}
		if len(state) != 2 {
			t.Fatalf("--match %q left %d rows, want 2", match, len(state))
		}
		for name, row := range state {
			if row.Description == "" {
				t.Errorf("--match %q blanked the description of %s", match, name)
			}
		}
		if _, err := os.Stat(filepath.Join(out, "acme", render.FilesDir, "web.tsv")); err != nil {
			t.Errorf("--match %q removed an inventory it did not select: %v", match, err)
		}
	}
}

// TestADroppedRowStillGoesWhenTheListingIsComplete: the fix must not cost the
// behaviour it protects.
func TestADroppedRowStillGoesWhenTheListingIsComplete(t *testing.T) {
	out := t.TempDir()
	if _, err := Build(context.Background(), newSource(), &fakeClient{}, options(out)); err != nil {
		t.Fatal(err)
	}
	shrunk := newSource()
	shrunk.repos = shrunk.repos[:1]
	summary, err := Build(context.Background(), shrunk, &fakeClient{}, options(out))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Dropped != 1 {
		t.Errorf("dropped = %d, want 1", summary.Dropped)
	}
}
