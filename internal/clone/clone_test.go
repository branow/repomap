package clone

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
	calls  [][]string
	isRepo bool
	err    error
	// answerHeads opts a test into ls-remote succeeding, so the probe that tells
	// an empty repository from an unreadable one can be driven both ways. Left
	// off, ls-remote fails like every other command, which is what a test about
	// an unreachable remote needs.
	answerHeads bool
	heads       string
}

func (f *fake) Run(_ context.Context, dir string, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if args[0] == "rev-parse" && len(args) > 1 && args[1] == "--is-inside-work-tree" {
		if f.isRepo {
			return "true", nil
		}
		return "", errors.New("not a git repository")
	}
	// ls-remote answers even when a clone failed: it is how an empty repository
	// is told apart from an unreadable one.
	if args[0] == "ls-remote" && f.answerHeads {
		return f.heads, nil
	}
	if f.err != nil {
		return "", f.err
	}
	return "", nil
}

func (f *fake) ran(prefix string) bool {
	for _, call := range f.calls {
		if strings.HasPrefix(strings.Join(call, " "), prefix) {
			return true
		}
	}
	return false
}

// TestADirectoryTheForgeReportedIsNeverWritten: that tree belongs to whoever
// put it there, and a build that reset it would throw away work in progress.
func TestADirectoryTheForgeReportedIsNeverWritten(t *testing.T) {
	runner := &fake{isRepo: true}
	store := &Store{Root: t.TempDir(), Git: runner}

	dir, err := store.Ensure(context.Background(),
		catalog.Repo{Name: "api", Dir: "/somebody-elses/api"})
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/somebody-elses/api" {
		t.Errorf("dir = %q, want the directory the forge reported", dir)
	}
	if len(runner.calls) != 0 {
		t.Errorf("git was run against somebody else's checkout: %v", runner.calls)
	}
}

func TestAnExistingCloneIsUpdatedNotRecloned(t *testing.T) {
	root := t.TempDir()
	runner := &fake{isRepo: true}
	store := &Store{Root: root, Git: runner}

	if _, err := store.Ensure(context.Background(), catalog.Repo{
		Name: "api", Remote: "git@example.internal:acme/api.git"}); err != nil {
		t.Fatal(err)
	}
	if !runner.ran("fetch") {
		t.Error("an existing clone was not fetched")
	}
	if runner.ran("clone") {
		t.Error("an existing clone was cloned again")
	}
}

// TestAHalfWrittenCloneIsReplaced: a directory that exists and is not a
// repository is what a killed run leaves behind, and this tree is a cache, so
// removing it is the only way forward and costs nothing.
func TestAHalfWrittenCloneIsReplaced(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "api")
	if err := os.MkdirAll(filepath.Join(broken, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "half.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &fake{isRepo: false}
	store := &Store{Root: root, Git: runner}
	if _, err := store.Ensure(context.Background(), catalog.Repo{
		Name: "api", Remote: "git@example.internal:acme/api.git"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(broken, "half.txt")); !os.IsNotExist(err) {
		t.Error("the half-written directory survived")
	}
	if !runner.ran("clone") {
		t.Error("the repository was not recloned")
	}
}

func TestARepositoryWithNothingToReadIsRefused(t *testing.T) {
	store := &Store{Root: t.TempDir(), Git: &fake{}}
	_, err := store.Ensure(context.Background(), catalog.Repo{Name: "ghost"})
	var missing *NoRemoteError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want a NoRemoteError", err)
	}
}

// TestACloneFailureNamesTheRepository, because a run skips it and carries on,
// and a summary that only counted them tells nobody which to go and look at.
func TestACloneFailureNamesTheRepository(t *testing.T) {
	runner := &fake{isRepo: false, err: errors.New("Permission denied (publickey).")}
	store := &Store{Root: t.TempDir(), Git: runner}

	_, err := store.Ensure(context.Background(), catalog.Repo{
		Name: "api", Remote: "git@example.internal:acme/api.git"})
	var failed *CloneError
	if !errors.As(err, &failed) {
		t.Fatalf("error = %v, want a CloneError", err)
	}
	if failed.Repo != "api" || !strings.Contains(err.Error(), "api") {
		t.Errorf("the failure does not name the repository: %v", err)
	}
}

// TestReachableIsTheUpFrontProbe: the failure it catches - no network, no VPN,
// a dead key - fails every repository identically, so it has to be one message
// rather than two hundred.
func TestReachableIsTheUpFrontProbe(t *testing.T) {
	runner := &fake{err: errors.New("Could not resolve hostname")}
	store := &Store{Root: t.TempDir(), Git: runner}

	err := store.Reachable(context.Background(), catalog.Repo{
		Name: "api", Remote: "git@example.internal:acme/api.git"})
	var access *AccessError
	if !errors.As(err, &access) {
		t.Fatalf("error = %v, want an AccessError", err)
	}
	if !strings.Contains(err.Error(), "VPN") {
		t.Errorf("the message does not name the usual cause: %v", err)
	}
	// A repository with no remote has nothing to probe.
	if err := store.Reachable(context.Background(), catalog.Repo{Name: "local"}); err != nil {
		t.Errorf("a local checkout was probed: %v", err)
	}
}

func TestDefaultRootIsPerWorkspace(t *testing.T) {
	one, err := DefaultRoot("platform")
	if err != nil {
		t.Skip("no user cache directory on this machine")
	}
	other, _ := DefaultRoot("tools")
	if one == other {
		t.Error("two workspaces share a clone directory, so their repositories collide")
	}
	if !strings.Contains(one, "repomap") {
		t.Errorf("clones land outside this tool's own directory: %s", one)
	}
}

// TestAnEmptyRepositoryIsNotABrokenOne pins what a real Bitbucket account
// showed: a forge names a default branch for a repository with no commits -
// Bitbucket says "master" - so the clone fails on a branch nobody ever created,
// and the run reported two perfectly healthy empty repositories as broken.
func TestAnEmptyRepositoryIsNotABrokenOne(t *testing.T) {
	// The clone fails on a branch that was never created, and ls-remote answers
	// with nothing: the repository is empty.
	runner := &fake{isRepo: false, answerHeads: true, heads: "", err: errors.New(
		"fatal: Remote branch master not found in upstream origin")}
	store := &Store{Root: t.TempDir(), Git: runner}

	_, err := store.Ensure(context.Background(), catalog.Repo{
		Name: "fresh", Remote: "git@example.internal:acme/fresh.git", Branch: "master"})

	var empty *EmptyError
	if !errors.As(err, &empty) {
		t.Fatalf("error = %v, want an EmptyError", err)
	}
	if !strings.Contains(err.Error(), "no commits") {
		t.Errorf("message = %q, want it to say the repository is empty", err)
	}
}

// And a repository that genuinely cannot be read stays broken.
func TestACloneFailureWithBranchesIsStillBroken(t *testing.T) {
	runner := &fake{isRepo: false, answerHeads: true, heads: "abc123\trefs/heads/main",
		err: errors.New("Permission denied (publickey).")}
	store := &Store{Root: t.TempDir(), Git: runner}

	_, err := store.Ensure(context.Background(), catalog.Repo{
		Name: "api", Remote: "git@example.internal:acme/api.git"})

	var failed *CloneError
	if !errors.As(err, &failed) {
		t.Fatalf("error = %v, want a CloneError", err)
	}
}
