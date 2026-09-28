package plan

import (
	"testing"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/fingerprint"
)

func repo(name string, activity string) catalog.Repo {
	return catalog.Repo{Name: name, Activity: catalog.Signal(activity)}
}

func TestScanReasons(t *testing.T) {
	state := map[string]catalog.State{
		"steady":      {Activity: "2026-01-01", Fingerprint: "aaaa", Description: "a thing"},
		"pushed":      {Activity: "2026-01-01", Fingerprint: "bbbb", Description: "a thing"},
		"blank":       {Activity: "2026-01-01", Fingerprint: "cccc", Description: ""},
		"gone":        {Activity: "2026-01-01", Fingerprint: "dddd", Description: "a thing"},
		"unsignalled": {Fingerprint: "eeee", Description: "a thing"},
	}
	repos := []catalog.Repo{
		repo("steady", "2026-01-01"),
		repo("pushed", "2026-06-01"),
		repo("blank", "2026-01-01"),
		repo("fresh", "2026-06-01"),
		repo("unsignalled", ""),
		{Name: "frozen", Activity: "2026-01-01", Archived: true},
	}

	plan := Scan(repos, state)

	want := map[string]Reason{
		"pushed":      Moved,
		"blank":       Undescribed,
		"fresh":       New,
		"unsignalled": Moved,
	}
	for name, reason := range want {
		if plan.Reasons[name] != reason {
			t.Errorf("%s: reason = %q, want %q", name, plan.Reasons[name], reason)
		}
	}
	if len(plan.Scan) != len(want) {
		t.Errorf("scanning %d repositories, want %d", len(plan.Scan), len(want))
	}
	if len(plan.Reuse) != 1 || plan.Reuse[0].Name != "steady" {
		t.Errorf("reuse = %v, want only steady", names(plan.Reuse))
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0].Name != "frozen" {
		t.Errorf("skipped = %v, want only frozen", names(plan.Skipped))
	}
	if len(plan.Dropped) != 1 || plan.Dropped[0] != "gone" {
		t.Errorf("dropped = %v, want [gone]", plan.Dropped)
	}
}

// TestAnAbsentSignalNeverProvesAnythingUntouched: a forge with no activity
// stamp - the local one - degrades to a single tier rather than being
// special-cased by name, and a repository it reports always rescans.
func TestAnAbsentSignalNeverProvesAnythingUntouched(t *testing.T) {
	state := map[string]catalog.State{"api": {Fingerprint: "aaaa", Description: "a thing"}}
	plan := Scan([]catalog.Repo{{Name: "api"}}, state)
	if len(plan.Scan) != 1 || plan.Reasons["api"] != Moved {
		t.Errorf("a repository with no signal was treated as untouched: %+v", plan)
	}
}

// TestAnEmptyDescriptionIsRepaired pins the rule that keeps a failed describe
// from being permanent: without it the signal matches next run, the row counts
// as untouched, and the column stays empty forever.
func TestAnEmptyDescriptionIsRepaired(t *testing.T) {
	state := map[string]catalog.State{
		"api": {Activity: "2026-01-01", Fingerprint: "aaaa", Description: ""},
	}
	plan := Scan([]catalog.Repo{repo("api", "2026-01-01")}, state)
	if len(plan.Scan) != 1 {
		t.Fatal("a row with no description was left alone")
	}
	if plan.Reasons["api"] != Undescribed {
		t.Errorf("reason = %q, want %q", plan.Reasons["api"], Undescribed)
	}
}

// TestAnArchivedRepositoryIsNeverScanned: it stays in the catalog, because
// "this exists and is frozen" is the answer a reader looking for it needs, and
// costs nothing to keep.
func TestAnArchivedRepositoryIsNeverScanned(t *testing.T) {
	plan := Scan([]catalog.Repo{{Name: "old", Archived: true}}, nil)
	if len(plan.Scan) != 0 {
		t.Error("an archived repository was scanned")
	}
	if len(plan.Skipped) != 1 {
		t.Error("an archived repository was dropped rather than skipped")
	}
	if len(plan.Dropped) != 0 {
		t.Error("an archived repository was marked as gone")
	}
}

func entry(name string, paths ...string) catalog.Entry {
	tree := catalog.Tree{Branch: "main"}
	for _, path := range paths {
		tree.Files = append(tree.Files, catalog.File{Path: path, Lines: 10})
	}
	return catalog.Entry{Repo: catalog.Repo{Name: name}, Tree: tree}
}

func TestDescribeSkipsWhatTheFingerprintProvesUnchanged(t *testing.T) {
	unchanged := entry("api", "main.go")
	changed := entry("web", "main.go")

	state := map[string]catalog.State{
		"api": {
			Fingerprint: fingerprint.Of(unchanged.Repo, unchanged.Tree),
			Description: "the api",
			Files:       map[string]string{"main.go": "entry point"},
		},
		"web": {Fingerprint: "stale", Description: "the web app"},
	}

	plan := Describe([]catalog.Entry{unchanged, changed}, state)

	if len(plan.Describe) != 1 || plan.Describe[0].Key() != "web" {
		t.Fatalf("describing %v, want only web", keys(plan.Describe))
	}
	if len(plan.Unchanged) != 1 || plan.Unchanged[0].Description != "the api" {
		t.Fatalf("unchanged = %v, want api with its description kept", keys(plan.Unchanged))
	}
	if got := plan.Unchanged[0].Tree.Files[0].Purpose; got != "entry point" {
		t.Errorf("per-file phrase = %q, want it carried forward", got)
	}
	// Every entry leaves with the hash just computed, so the writer records the
	// state the reader will be compared against next run.
	for _, e := range append(plan.Describe, plan.Unchanged...) {
		if e.Fingerprint == "" || e.Fingerprint == "stale" {
			t.Errorf("%s left without a freshly computed fingerprint", e.Key())
		}
	}
}

// TestAMatchingHashWithNoDescriptionIsStillDescribed: a hash that matches
// proves the content is the same, not that anybody ever wrote a sentence
// about it.
func TestAMatchingHashWithNoDescriptionIsStillDescribed(t *testing.T) {
	e := entry("api", "main.go")
	state := map[string]catalog.State{
		"api": {Fingerprint: fingerprint.Of(e.Repo, e.Tree), Description: ""},
	}
	if plan := Describe([]catalog.Entry{e}, state); len(plan.Describe) != 1 {
		t.Error("a row with a matching hash and no sentence was left empty")
	}
}

// TestAPhraseForARemovedFileIsDropped: restore matches by path, so a phrase
// that finds no file goes with the file.
func TestAPhraseForARemovedFileIsDropped(t *testing.T) {
	e := entry("api", "main.go")
	state := map[string]catalog.State{
		"api": {
			Fingerprint: fingerprint.Of(e.Repo, e.Tree),
			Description: "the api",
			Files:       map[string]string{"main.go": "entry point", "deleted.go": "gone"},
		},
	}
	plan := Describe([]catalog.Entry{e}, state)
	if len(plan.Unchanged[0].Tree.Files) != 1 {
		t.Fatalf("file list grew to %d", len(plan.Unchanged[0].Tree.Files))
	}
}

func names(repos []catalog.Repo) []string {
	out := make([]string, len(repos))
	for i, r := range repos {
		out[i] = r.Name
	}
	return out
}

func keys(entries []catalog.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Key()
	}
	return out
}
