// Package clone keeps the working trees a build reads. It is the only place
// that decides where a repository lives on this machine, and the only one that
// runs a git command that touches a network.
package clone

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/git"
)

// Store materialises working trees. A repository the forge already located on
// disk is used where it is; one with a remote is cloned into the store's root
// and updated in place afterwards.
type Store struct {
	// Root is where clones live. A repository reported with its own directory
	// never goes here.
	Root string
	Git  git.Runner
}

// New builds a store over the real git binary.
func New(root string) *Store { return &Store{Root: root, Git: git.CLI{}} }

// Dir is where a repository's working tree is, without touching the network. It
// is what the scanner uses when the build has decided this repository need not
// be refreshed.
func (s *Store) Dir(repo catalog.Repo) string {
	if repo.Dir != "" {
		return repo.Dir
	}
	return filepath.Join(s.Root, repo.Name)
}

// Ensure brings a repository's working tree up to date and returns where it is.
// A repository the forge reported with a directory is never written to: it
// belongs to whoever put it there, and a build that reset it would throw away
// somebody's work in progress.
func (s *Store) Ensure(ctx context.Context, repo catalog.Repo) (string, error) {
	dir := s.Dir(repo)
	if repo.Dir != "" {
		return dir, nil
	}
	if repo.Remote == "" {
		return "", &NoRemoteError{Repo: repo.Name}
	}

	switch {
	case git.IsRepo(ctx, s.Git, dir):
		if err := git.Update(ctx, s.Git, dir); err != nil {
			return "", &UpdateError{Repo: repo.Name, Dir: dir, Err: err}
		}
	default:
		// A half-written clone from a killed run is a directory that exists and
		// is not a repository. Removing it is the only way forward, and it holds
		// nothing of anyone's: this tree is a cache.
		if err := os.RemoveAll(dir); err != nil {
			return "", &UpdateError{Repo: repo.Name, Dir: dir, Err: err}
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", &UpdateError{Repo: repo.Name, Dir: dir, Err: err}
		}
		if err := git.Clone(ctx, s.Git, repo.Remote, dir, repo.Branch); err != nil {
			return "", &CloneError{Repo: repo.Name, Remote: repo.Remote, Err: err}
		}
	}
	return dir, nil
}

// Reachable probes one remote before a run clones anything, so an expired key
// or a dropped VPN is one message up front rather than a failure per
// repository. It answers for the forge as a whole, which is why the caller
// probes a single repository and not all of them.
func (s *Store) Reachable(ctx context.Context, repo catalog.Repo) error {
	if repo.Remote == "" {
		return nil
	}
	if err := git.Reachable(ctx, s.Git, repo.Remote); err != nil {
		return &AccessError{Remote: repo.Remote, Err: err}
	}
	return nil
}

// DefaultRoot is where clones go when a workspace does not say. It is under the
// user's cache directory rather than their home or the index tree: these are
// reconstructible from the forge, and anything that clears a cache is welcome
// to clear them.
func DefaultRoot(workspace string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return "", err
		}
		base = filepath.Join(home, ".cache")
		if runtime.GOOS == "windows" {
			base = home
		}
	}
	return filepath.Join(base, "repomap", "clones", workspace), nil
}
