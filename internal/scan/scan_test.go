package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/branow/repomap/internal/catalog"
)

type fake struct {
	answers map[string]string
	fail    bool
}

func (f *fake) Run(_ context.Context, _ string, args ...string) (string, error) {
	if f.fail {
		return "", errors.New("not a git repository")
	}
	return f.answers[strings.Join(args, " ")], nil
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	write(t, dir, "internal/router.go", "package internal\n")
	write(t, dir, "README.md", "# the api\n\nIt places orders.\n")

	runner := &fake{answers: map[string]string{
		"rev-parse HEAD":              "abc123",
		"ls-files -z":                 "internal/router.go\x00main.go\x00README.md\x00",
		"rev-parse --abbrev-ref HEAD": "main",
		"log -1 --format=%cs":         "2026-09-28",
	}}

	tree, err := Scan(context.Background(), runner, dir)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Head != "abc123" || tree.Branch != "main" || tree.LastCommit != "2026-09-28" {
		t.Errorf("tree = %+v", tree)
	}
	if !strings.Contains(tree.Readme, "It places orders.") {
		t.Errorf("readme = %q", tree.Readme)
	}
	// Sorted, so neither the fingerprint nor the file a reader opens depends on
	// the order git happened to list them in.
	if tree.Files[0].Path != "README.md" || tree.Files[2].Path != "main.go" {
		t.Errorf("files are not sorted: %+v", tree.Files)
	}
	if tree.Files[2].Lines != 3 {
		t.Errorf("main.go has %d lines, want 3", tree.Files[2].Lines)
	}
	if tree.Bytes == 0 {
		t.Error("no size was recorded")
	}
}

// TestARepositoryWithNoCommitsIsNotAFailure: it is a normal state for a freshly
// created repository, and failing a build over one would stop a run that has
// two hundred others to index.
func TestARepositoryWithNoCommitsIsNotAFailure(t *testing.T) {
	_, err := Scan(context.Background(), &fake{fail: true}, t.TempDir())
	var empty *EmptyError
	if !errors.As(err, &empty) {
		t.Fatalf("error = %v, want an EmptyError", err)
	}
}

func TestCountLines(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "trailing newline", body: "a\nb\nc\n", want: 3},
		// A reader comparing this with their editor would otherwise find it off
		// by one on most short files.
		{name: "no trailing newline", body: "a\nb\nc", want: 3},
		{name: "one line", body: "a", want: 1},
		// An empty file genuinely has no lines; the +1 below is for a last
		// line that exists and simply is not newline-terminated.
		{name: "empty", body: "", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			write(t, dir, tt.name, tt.body)
			if got := countLines(filepath.Join(dir, tt.name)); got != tt.want {
				t.Errorf("countLines(%q) = %d, want %d", tt.body, got, tt.want)
			}
		})
	}
}

func TestOnlyTheRootReadmeIsRead(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "docs/README.md", "# the docs directory")
	if got := readme(dir); got != "" {
		t.Errorf("readme = %q, want none: a README two directories down describes that directory", got)
	}
	write(t, dir, "README.md", "# the repository")
	if got := readme(dir); got != "# the repository" {
		t.Errorf("readme = %q", got)
	}
}

func TestAHugeReadmeIsCut(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "README.md", strings.Repeat("x", catalog.MaxReadme*3))
	if got := len(readme(dir)); got != catalog.MaxReadme {
		t.Errorf("readme is %d characters, want it cut at %d", got, catalog.MaxReadme)
	}
}

func files(paths ...string) []catalog.File {
	out := make([]catalog.File, len(paths))
	for i, path := range paths {
		out[i] = catalog.File{Path: path}
	}
	return out
}

func TestStructure(t *testing.T) {
	got := Structure(files(
		"cmd/repomap/main.go",
		"internal/catalog/repo.go",
		"internal/render/tsv.go",
		"internal/render/roster.go",
		"go.mod",
		"README.md",
	))
	want := strings.Join([]string{
		"cmd/",
		"  repomap/",
		"internal/",
		"  catalog/",
		"  render/",
		"README.md",
		"go.mod",
	}, "\n")
	if got != want {
		t.Errorf("structure =\n%s\n\nwant\n%s", got, want)
	}
}

// TestStructureCapsAWideDirectory: a repository with three hundred packages
// under one directory says everything it has to say in the first handful, and
// the rest is prompt budget spent on a list.
func TestStructureCapsAWideDirectory(t *testing.T) {
	var paths []string
	for i := 0; i < MaxSubdirs+12; i++ {
		paths = append(paths, "packages/p"+string(rune('a'+i))+"/index.ts")
	}
	got := Structure(files(paths...))
	if !strings.Contains(got, "... 12 more") {
		t.Errorf("a wide directory was not capped:\n%s", got)
	}
	if lines := strings.Count(got, "\n") + 1; lines != MaxSubdirs+2 {
		t.Errorf("structure is %d lines, want %d", lines, MaxSubdirs+2)
	}
}

func TestStructureOfNothing(t *testing.T) {
	if got := Structure(nil); got != "(no files)" {
		t.Errorf("structure = %q", got)
	}
}
