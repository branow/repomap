package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fake records what was asked of git and answers from a table. No test in this
// package runs the real binary.
type fake struct {
	answers map[string]string
	err     error
	calls   [][]string
}

func (f *fake) Run(_ context.Context, dir string, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return "", f.err
	}
	return f.answers[strings.Join(args, " ")], nil
}

func TestFilesSplitsOnNUL(t *testing.T) {
	runner := &fake{answers: map[string]string{
		"ls-files -z": "main.go\x00internal/router.go\x00a file with spaces.md\x00",
	}}
	got, err := Files(context.Background(), runner, "/clone")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"main.go", "internal/router.go", "a file with spaces.md"}
	if len(got) != len(want) {
		t.Fatalf("listed %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestFilesListsTrackedPathsOnly: an index that walked the directory would
// carry build output and vendored dependencies, and .gitignore already says
// which is which.
func TestFilesListsTrackedPathsOnly(t *testing.T) {
	runner := &fake{answers: map[string]string{"ls-files -z": ""}}
	if _, err := Files(context.Background(), runner, "/clone"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.calls[0], " "); !strings.HasPrefix(got, "ls-files") {
		t.Errorf("listed files with %q, want ls-files", got)
	}
}

// TestCloneIsShallow: the index reads the tip and nothing else, so a full
// history is minutes and gigabytes bought for a question nobody asks here.
func TestCloneIsShallow(t *testing.T) {
	runner := &fake{answers: map[string]string{}}
	if err := Clone(context.Background(), runner,
		"git@example.internal:acme/api.git", "/clones/api", "main"); err != nil {
		t.Fatal(err)
	}
	args := strings.Join(runner.calls[0], " ")
	for _, want := range []string{"--depth=1", "--single-branch", "--no-tags", "--branch main"} {
		if !strings.Contains(args, want) {
			t.Errorf("clone args %q are missing %q", args, want)
		}
	}
}

// TestUpdateFallsBackToFetchHead: a clone made with --single-branch has no
// origin/HEAD, and FETCH_HEAD is the commit the fetch just wrote.
func TestUpdateFallsBackToFetchHead(t *testing.T) {
	runner := &fake{answers: map[string]string{
		"fetch --depth=1 --no-tags origin":   "",
		"rev-parse --abbrev-ref origin/HEAD": "",
	}}
	if err := Update(context.Background(), runner, "/clones/api"); err != nil {
		t.Fatal(err)
	}
	last := strings.Join(runner.calls[len(runner.calls)-1], " ")
	if last != "reset --hard FETCH_HEAD" {
		t.Errorf("final call = %q, want a reset to FETCH_HEAD", last)
	}
}

func TestBranchOnADetachedHeadIsEmpty(t *testing.T) {
	runner := &fake{answers: map[string]string{"rev-parse --abbrev-ref HEAD": "HEAD"}}
	branch, err := Branch(context.Background(), runner, "/clone")
	if err != nil || branch != "" {
		t.Errorf("branch = %q, err = %v, want empty and no error", branch, err)
	}
}

func TestIsRepoIsFalseForAnythingElse(t *testing.T) {
	yes := &fake{answers: map[string]string{"rev-parse --is-inside-work-tree": "true"}}
	if !IsRepo(context.Background(), yes, "/clone") {
		t.Error("a working tree was not recognised")
	}
	no := &fake{err: errors.New("not a git repository")}
	if IsRepo(context.Background(), no, "/not-a-clone") {
		t.Error("a plain directory was treated as a working tree")
	}
}

// TestACommandErrorCarriesGitsOwnDiagnosis: rewriting it into our own words is
// how "Permission denied (publickey)" becomes "clone failed".
func TestACommandErrorCarriesGitsOwnDiagnosis(t *testing.T) {
	err := &CommandError{
		Args:   []string{"clone", "git@example.internal:acme/api.git"},
		Dir:    "/clones",
		Stderr: "Permission denied (publickey).",
		Err:    errors.New("exit status 128"),
	}
	if !strings.Contains(err.Error(), "Permission denied (publickey).") {
		t.Errorf("git's own words were dropped: %s", err)
	}
}

func TestAMissingGitNamesItself(t *testing.T) {
	err := &MissingError{Err: errors.New("executable file not found in $PATH")}
	if !strings.Contains(err.Error(), "PATH") {
		t.Errorf("the message does not say where to look: %s", err)
	}
}
