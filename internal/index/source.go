package index

import (
	"context"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/clone"
	"github.com/branow/repomap/internal/describe"
	"github.com/branow/repomap/internal/forge"
	"github.com/branow/repomap/internal/scan"
)

// Source is the live half of a build: where the repository list comes from and
// where the working trees are. Close is not cleanup - the build calls it before
// the first model call, so a describe run never holds anything open across
// minutes of third-party latency.
type Source interface {
	// Forge and Slug name what this source is, for the roster row.
	Forge() string
	Slug() string

	Repositories(ctx context.Context) ([]catalog.Repo, error)
	// Reachable probes access once, before anything is cloned.
	Reachable(ctx context.Context, repo catalog.Repo) error
	// Scan brings one repository's working tree up to date and reads it.
	Scan(ctx context.Context, repo catalog.Repo) (catalog.Tree, error)

	Close() error
}

// source joins a forge listing to the clone store the trees are read from.
type source struct {
	forge forge.Forge
	store *clone.Store
	slug  string
}

// Open builds a Source, performing no I/O.
func Open(f forge.Forge, store *clone.Store, slug string) Source {
	return &source{forge: f, store: store, slug: slug}
}

func (s *source) Forge() string { return s.forge.Name() }

func (s *source) Slug() string { return s.slug }

func (s *source) Repositories(ctx context.Context) ([]catalog.Repo, error) {
	return s.forge.Repositories(ctx)
}

func (s *source) Reachable(ctx context.Context, repo catalog.Repo) error {
	return s.store.Reachable(ctx, repo)
}

func (s *source) Scan(ctx context.Context, repo catalog.Repo) (catalog.Tree, error) {
	dir, err := s.store.Ensure(ctx, repo)
	if err != nil {
		return catalog.Tree{}, err
	}
	return scan.Scan(ctx, s.store.Git, dir)
}

// Close releases nothing today: a clone store holds no handle. It is on the
// interface because the ordering it pins - every network operation finished
// before the first model call - is a property of the build worth keeping true
// when a future source does hold one.
func (s *source) Close() error { return nil }

// Logger reports what a build is doing and what it skipped.
type Logger interface {
	Info(message string)
	Warn(message string)
}

func info(logger Logger, message string) {
	if logger != nil {
		logger.Info(message)
	}
}

func warn(logger Logger, message string) {
	if logger != nil {
		logger.Warn(message)
	}
}

// loggerFor hands this package's logger to the describe stage. The two
// interfaces are the same shape, declared separately so neither package
// imports the other for a two-method contract.
func loggerFor(logger Logger) describe.Logger {
	if logger == nil {
		return nil
	}
	return logger
}
