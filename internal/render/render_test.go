package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/branow/repomap/internal/catalog"
)

func entry(name, description, commit string, paths ...string) catalog.Entry {
	tree := catalog.Tree{LastCommit: commit, Branch: "main"}
	for _, path := range paths {
		tree.Files = append(tree.Files, catalog.File{Path: path, Lines: 10, Bytes: 1024})
		tree.Bytes += 1024
	}
	return catalog.Entry{
		Repo:        catalog.Repo{Name: name, Activity: catalog.Signal(commit + "T00:00:00Z")},
		Tree:        tree,
		Fingerprint: "hash-" + name,
		Description: description,
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// TestANarrowedRunMergesRatherThanReplaces is the property that makes --match
// and --limit safe. Rewriting the catalog from one run's selection would delete
// the fingerprint of every repository it did not look at, so the next full
// build would see them all as new and buy every description again.
func TestANarrowedRunMergesRatherThanReplaces(t *testing.T) {
	dir := t.TempDir()

	if _, err := Catalog(dir, []catalog.Entry{
		entry("api", "the api", "2026-01-01", "main.go"),
		entry("web", "the web app", "2026-01-02", "app.tsx"),
	}, nil); err != nil {
		t.Fatal(err)
	}

	// A second run that only covered `api`.
	if _, err := Catalog(dir, []catalog.Entry{
		entry("api", "the api, rewritten", "2026-02-01", "main.go", "router.go"),
	}, nil); err != nil {
		t.Fatal(err)
	}

	state, err := State(dir)
	if err != nil {
		t.Fatal(err)
	}
	if state["web"].Description != "the web app" {
		t.Errorf("web lost its description: %q", state["web"].Description)
	}
	if state["web"].Fingerprint != "hash-web" {
		t.Errorf("web lost its fingerprint: %q", state["web"].Fingerprint)
	}
	if state["api"].Description != "the api, rewritten" {
		t.Errorf("api was not updated: %q", state["api"].Description)
	}
}

func TestDroppedRowsAreRemoved(t *testing.T) {
	dir := t.TempDir()
	if _, err := Catalog(dir, []catalog.Entry{
		entry("api", "the api", "2026-01-01"),
		entry("gone", "removed upstream", "2026-01-01"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Catalog(dir, nil, []string{"gone"}); err != nil {
		t.Fatal(err)
	}
	state, _ := State(dir)
	if _, ok := state["gone"]; ok {
		t.Error("a repository the forge no longer has kept its row")
	}
	if _, ok := state["api"]; !ok {
		t.Error("an untouched repository was dropped with it")
	}
}

// TestRowsAreSortedByName: the file must not depend on which repositories a run
// happened to select, or every narrowed build produces a diff nobody can read.
func TestRowsAreSortedByName(t *testing.T) {
	dir := t.TempDir()
	if _, err := Catalog(dir, []catalog.Entry{
		entry("zulu", "z", "2026-01-01"),
		entry("alpha", "a", "2026-01-01"),
		entry("mike", "m", "2026-01-01"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	body := read(t, filepath.Join(dir, CatalogFile))
	if strings.Index(body, "alpha") > strings.Index(body, "mike") ||
		strings.Index(body, "mike") > strings.Index(body, "zulu") {
		t.Errorf("rows are not sorted:\n%s", body)
	}
}

// TestASkippedRowIsNotADescription: reading the placeholder back as one would
// make an archived repository look described, and one that came out of the
// archive would never earn a real sentence.
func TestASkippedRowIsNotADescription(t *testing.T) {
	dir := t.TempDir()
	archived := catalog.Entry{
		Repo: catalog.Repo{Name: "old", Archived: true},
		Skip: "archived",
	}
	if _, err := Catalog(dir, []catalog.Entry{archived}, nil); err != nil {
		t.Fatal(err)
	}
	if body := read(t, filepath.Join(dir, CatalogFile)); !strings.Contains(body, "(archived)") {
		t.Errorf("the skip reason did not reach the reader's column:\n%s", body)
	}
	state, _ := State(dir)
	if state["old"].Description != "" {
		t.Errorf("the placeholder was read back as a description: %q", state["old"].Description)
	}
}

// TestADescriptionCannotBreakTheRow: the sentence is written by a model, so it
// can contain a tab or a newline, and one that ended a row early would corrupt
// every row after it.
func TestADescriptionCannotBreakTheRow(t *testing.T) {
	dir := t.TempDir()
	hostile := entry("api", "one\ttwo\nthree\r\nfour", "2026-01-01")
	if _, err := Catalog(dir, []catalog.Entry{hostile}, nil); err != nil {
		t.Fatal(err)
	}
	body := read(t, filepath.Join(dir, CatalogFile))
	if lines := strings.Count(strings.TrimSpace(body), "\n"); lines != 1 {
		t.Errorf("a description broke the file into %d extra lines:\n%s", lines, body)
	}
	state, _ := State(dir)
	if state["api"].Description != "one two three four" {
		t.Errorf("description = %q", state["api"].Description)
	}
}

func TestFilesCarryTheFingerprintAndEveryPath(t *testing.T) {
	dir := t.TempDir()
	e := entry("api", "the api", "2026-01-01", "main.go", "internal/router.go")
	e.Tree.Files[0].Purpose = "process entry point"

	if err := Files(dir, e); err != nil {
		t.Fatal(err)
	}
	body := read(t, filepath.Join(dir, "api.tsv"))

	if !strings.HasPrefix(body, "# api @hash-api\n") {
		t.Errorf("the header does not name the state these rows describe:\n%s", body)
	}
	for _, want := range []string{"main.go", "internal/router.go", "process entry point"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q is missing from the inventory:\n%s", want, body)
		}
	}
	// A phrase is read back so an unchanged run keeps it.
	if phrases := readPhrases(filepath.Join(dir, "api.tsv")); phrases["main.go"] != "process entry point" {
		t.Errorf("phrases read back = %v", phrases)
	}
}

func TestPruneRemovesInventoriesOfGoneRepositories(t *testing.T) {
	dir := t.TempDir()
	if err := Files(dir, entry("gone", "x", "2026-01-01", "main.go")); err != nil {
		t.Fatal(err)
	}
	if err := Prune(dir, []string{"gone", "never-existed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.tsv")); !os.IsNotExist(err) {
		t.Error("the inventory of a removed repository survived")
	}
}

func TestActiveKeepsOnlyRecentRepositories(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	if _, err := Catalog(dir, []catalog.Entry{
		entry("current", "recent", "2026-06-01"),
		entry("ancient", "old", "2019-01-01"),
		entry("undated", "no commit date", ""),
	}, nil); err != nil {
		t.Fatal(err)
	}

	kept, total, err := Active(dir, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	body := read(t, filepath.Join(dir, ActiveFile))
	if strings.Contains(body, "ancient") {
		t.Error("a repository nobody has touched in seven years is in the active view")
	}
	// A row that cannot be dated is kept: the view hides what is certainly
	// dead, and an undated row is not certainly anything.
	if !strings.Contains(body, "undated") || kept != 2 {
		t.Errorf("kept = %d, want 2 including the undated row:\n%s", kept, body)
	}
}

// TestTheRosterMergesToo: a tree holds several workspaces built at different
// times by different people, so a build that rewrote the file would erase the
// stamps of the ones it did not touch.
func TestTheRosterMergesToo(t *testing.T) {
	root := t.TempDir()
	first := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	second := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	if err := WriteRoster(root, Roster{Workspace: "platform", Forge: "github",
		Slug: "acme", Repos: 12, Built: first}); err != nil {
		t.Fatal(err)
	}
	if err := WriteRoster(root, Roster{Workspace: "tools", Forge: "bitbucket",
		Slug: "acme-tools", Repos: 4, Built: second}); err != nil {
		t.Fatal(err)
	}

	rows, err := ReadRoster(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("roster holds %d rows, want 2", len(rows))
	}
	for _, row := range rows {
		if row.Workspace == "platform" && !row.Built.Equal(first) {
			t.Errorf("the first workspace's stamp moved to %v", row.Built)
		}
	}
}

func TestSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: 512, want: "512 B"},
		{bytes: 2048, want: "2 KB"},
		{bytes: 5 << 20, want: "5 MB"},
		{bytes: 3 << 30, want: "3 GB"},
	}
	for _, tt := range tests {
		if got := Size(tt.bytes); got != tt.want {
			t.Errorf("Size(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

// TestTheGuideExplainsTheTreeItSitsIn: the reader is not the builder, so every
// file name the tree uses has to appear in the reference beside it.
func TestTheGuideExplainsTheTreeItSitsIn(t *testing.T) {
	root := t.TempDir()
	if err := WriteGuide(root); err != nil {
		t.Fatal(err)
	}
	body := read(t, filepath.Join(root, GuideFile))
	for _, name := range []string{RosterFile, CatalogFile, ActiveFile, FilesDir} {
		if !strings.Contains(body, name) {
			t.Errorf("the guide never mentions %q", name)
		}
	}
}

func TestAMissingTreeIsNotAFailure(t *testing.T) {
	state, err := State(filepath.Join(t.TempDir(), "never-built"))
	if err != nil {
		t.Fatalf("reading an unbuilt tree: %v", err)
	}
	if len(state) != 0 {
		t.Error("an unbuilt tree produced state")
	}
}

// TestTheCatalogRecordsWhereTheCodeIs: without the clone url a reader who pulled
// a published index can be told which file to open and still have no way to open
// it, because they have no forge credential and no binary.
func TestTheCatalogRecordsWhereTheCodeIs(t *testing.T) {
	dir := t.TempDir()
	e := entry("api", "the api", "2026-01-01", "main.go")
	e.Repo.Remote = "git@example.internal:acme/api.git"

	if _, err := Catalog(dir, []catalog.Entry{e}, nil); err != nil {
		t.Fatal(err)
	}
	if body := read(t, filepath.Join(dir, CatalogFile)); !strings.Contains(body, e.Repo.Remote) {
		t.Errorf("the clone url is not in the catalog:\n%s", body)
	}
	state, err := State(dir)
	if err != nil {
		t.Fatal(err)
	}
	if state["api"].Remote != e.Repo.Remote {
		t.Errorf("remote read back as %q", state["api"].Remote)
	}
	// A row carried forward untouched must keep it.
	if _, err := Catalog(dir, nil, nil); err != nil {
		t.Fatal(err)
	}
	if again, _ := State(dir); again["api"].Remote != e.Repo.Remote {
		t.Error("a merge dropped the clone url")
	}
}
