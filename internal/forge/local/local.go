// Package local lists the repositories already checked out in a directory: the
// forge with no API, no credential and no network. It is what lets somebody try
// the tool before granting it access to anything, and what points it at a
// checkout tree something else already maintains.
package local

import (
	"context"
	"os"
	"path/filepath"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/forge"
	"github.com/branow/repomap/internal/git"
)

// Name is the value that selects this forge in a workspace entry.
const Name = "local"

func init() {
	forge.Register(Name, func(cfg forge.Config) (forge.Forge, error) {
		return &local{cfg: cfg, git: git.CLI{}}, nil
	})
}

type local struct {
	cfg forge.Config
	git git.Runner
}

// New builds a local forge over an explicit git runner, which is how a test
// drives it without checkouts on disk.
func New(cfg forge.Config, runner git.Runner) forge.Forge {
	return &local{cfg: cfg, git: runner}
}

func (l *local) Name() string { return Name }

// Repositories lists the immediate subdirectories of the configured directory
// that are git working trees. Only the immediate ones: a recursive walk of a
// developer's source directory finds every vendored dependency and every
// checkout of a checkout, and no depth limit makes that the right answer.
func (l *local) Repositories(ctx context.Context) ([]catalog.Repo, error) {
	entries, err := os.ReadDir(l.cfg.Dir)
	if err != nil {
		return nil, &DirectoryError{Dir: l.cfg.Dir, Err: err}
	}

	var repos []catalog.Repo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(l.cfg.Dir, entry.Name())
		if !git.IsRepo(ctx, l.git, dir) {
			continue
		}
		repos = append(repos, catalog.Repo{
			Name: entry.Name(),
			Dir:  dir,
			// No Activity: a working tree has no cheap stamp that a push moved
			// it, so every local repository is scanned every run. Scanning one
			// is a directory read, and the content fingerprint still decides
			// whether anything is described - the design degrades to one tier
			// rather than special-casing this forge by name.
		})
	}
	return l.cfg.Filter(repos), nil
}

// DirectoryError reports a configured directory that could not be read, which
// is the one failure this forge has.
type DirectoryError struct {
	Dir string
	Err error
}

func (e *DirectoryError) Error() string {
	return "cannot read the checkout directory " + e.Dir + ": " + e.Err.Error()
}

func (e *DirectoryError) Unwrap() error { return e.Err }
