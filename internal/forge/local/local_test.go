package local

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/branow/repomap/internal/forge"
)

// runner answers IsRepo from a set of directories it considers working trees.
type runner struct{ repos map[string]bool }

func (r runner) Run(_ context.Context, dir string, args ...string) (string, error) {
	if strings.Join(args, " ") == "rev-parse --is-inside-work-tree" && r.repos[dir] {
		return "true", nil
	}
	return "", errors.New("not a git repository")
}

func tree(t *testing.T, names ...string) (string, runner) {
	t.Helper()
	root := t.TempDir()
	repos := map[string]bool{}
	for _, name := range names {
		dir := filepath.Join(root, strings.TrimSuffix(name, "!"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// A trailing "!" marks a plain directory that is not a working tree.
		repos[dir] = !strings.HasSuffix(name, "!")
	}
	return root, runner{repos: repos}
}

func TestOnlyWorkingTreesAreListed(t *testing.T) {
	root, git := tree(t, "api", "web", "notes!")
	if err := os.WriteFile(filepath.Join(root, "loose.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := New(forge.Config{Dir: root}, git).Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("listed %d, want 2: %+v", len(got), got)
	}
	if got[0].Name != "api" || got[1].Name != "web" {
		t.Errorf("listed %+v, want api and web in name order", got)
	}
}

// TestACheckoutIsReportedWhereItIs: nothing is cloned and the directory is
// never written to, because it belongs to whoever put it there.
func TestACheckoutIsReportedWhereItIs(t *testing.T) {
	root, git := tree(t, "api")
	got, err := New(forge.Config{Dir: root}, git).Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Dir != filepath.Join(root, "api") {
		t.Errorf("dir = %q, want the checkout's own path", got[0].Dir)
	}
	if got[0].Remote != "" {
		t.Errorf("remote = %q, want none: there is nothing to clone", got[0].Remote)
	}
}

// TestThereIsNoActivitySignal: a working tree has no cheap stamp that a push
// moved it, so this forge degrades to a single staleness tier rather than being
// special-cased by name anywhere above.
func TestThereIsNoActivitySignal(t *testing.T) {
	root, git := tree(t, "api")
	got, _ := New(forge.Config{Dir: root}, git).Repositories(context.Background())
	if got[0].Activity.Present() {
		t.Errorf("activity = %q, want none", got[0].Activity)
	}
}

// TestOnlyImmediateSubdirectories: a recursive walk of a developer's source
// directory finds every vendored dependency and every checkout of a checkout,
// and no depth limit makes that the right answer.
func TestOnlyImmediateSubdirectories(t *testing.T) {
	root, git := tree(t, "group/api", "top")
	got, err := New(forge.Config{Dir: root}, git).Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "top" {
		t.Errorf("listed %+v, want only the immediate checkout", got)
	}
}

func TestAnUnreadableDirectoryIsNamed(t *testing.T) {
	_, err := New(forge.Config{Dir: "/no/such/place"}, runner{}).
		Repositories(context.Background())
	var missing *DirectoryError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want a DirectoryError", err)
	}
	if !strings.Contains(err.Error(), "/no/such/place") {
		t.Errorf("the error does not name the directory: %v", err)
	}
}

func TestExcludeApplies(t *testing.T) {
	root, git := tree(t, "api", "api-sandbox")
	got, err := New(forge.Config{Dir: root, Exclude: []string{"-sandbox"}}, git).
		Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "api" {
		t.Errorf("listed %+v, want only api", got)
	}
}
