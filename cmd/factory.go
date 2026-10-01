package cmd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/branow/gollm"
	"github.com/branow/gollm/provider"
	"github.com/branow/repomap/internal/clone"
	"github.com/branow/repomap/internal/cmdutil"
	"github.com/branow/repomap/internal/config"
	"github.com/branow/repomap/internal/credentials"
	"github.com/branow/repomap/internal/forge"
	"github.com/branow/repomap/internal/index"
	"github.com/branow/repomap/internal/iostreams"
)

// probeTimeout bounds a verification: generous for a round trip to a slow
// forge, short enough that a host silently dropping packets returns.
const probeTimeout = 15 * time.Second

// backendProbeTimeout is longer: a model call is not a handshake, and the
// claudecode provider starts a process before it answers.
const backendProbeTimeout = 90 * time.Second

// NewFactory builds the real dependency bundle. The secret store is opened
// later, in the root command's pre-run, because a flag decides which store.
func NewFactory() (*cmdutil.Factory, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return &cmdutil.Factory{
		IO:     iostreams.System(),
		Config: cfg,
		Probes: cmdutil.Probes{Workspace: verifyWorkspace, Backend: verifyBackend},
	}, nil
}

// verifyWorkspace is the cheapest real proof that a workspace works: list its
// repositories and stop at the first page. Nothing is cloned and no file is
// read.
func verifyWorkspace(ctx context.Context, name string, entry config.Workspace,
	secret credentials.Secret) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	source, err := openSource(name, entry, secret)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = source.Repositories(ctx)
	return err
}

// verifyBackend is the cheapest real proof that a backend answers: build the
// client and make one tiny schema-constrained call. Without this, `backend add`
// stored anything at all while advertising a --no-verify flag.
func verifyBackend(ctx context.Context, name string, entry config.Backend,
	secret credentials.Secret) error {
	client, err := newClient(entry, secret)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, backendProbeTimeout)
	defer cancel()
	_, err = client.Complete(ctx, gollm.Request{
		Prompt: "Reply with the word ok.",
		Schema: json.RawMessage(`{"type":"object","additionalProperties":false,` +
			`"required":["ok"],"properties":{"ok":{"type":"string"}}}`),
		MaxTokens: 64,
	})
	return err
}

// newClient builds the model client one backend entry names.
func newClient(entry config.Backend, secret credentials.Secret) (gollm.Client, error) {
	return provider.New(gollm.Config{
		Provider: string(entry.Provider),
		Model:    entry.Model,
		BaseURL:  entry.BaseURL,
		APIKey:   secret.Reveal(),
	})
}

// openSource assembles a build source from one workspace entry: the forge that
// lists repositories, and the store the working trees are read from.
func openSource(name string, entry config.Workspace,
	secret credentials.Secret) (index.Source, error) {

	f, err := forge.New(string(entry.Forge), forge.Config{
		Slug:    entry.Slug,
		BaseURL: entry.BaseURL,
		Dir:     entry.Dir,
		Exclude: entry.Exclude,
		Credential: forge.Credential{
			Username: entry.Username,
			Token:    secret.Reveal(),
		},
	})
	if err != nil {
		return nil, err
	}

	root := entry.Clones
	if root == "" {
		root, err = clone.DefaultRoot(name)
		if err != nil {
			return nil, err
		}
	}
	slug := entry.Slug
	if slug == "" {
		slug = entry.Dir
	}
	return index.Open(f, clone.New(root), slug), nil
}

// openStore assembles the secret store the resolved policy selects. The default
// policy refuses a plaintext file outright, so the environment path is what
// lets a headless run work with no keychain at all.
//
// interactive may be true only when a person is watching a terminal: the macOS
// keychain binds an item to the storing binary's code identity, so a rebuilt
// binary is asked to re-authorize, and a dialog nobody can see blocks forever.
func openStore(c *config.Config, o config.Overrides, interactive bool) (credentials.Store, error) {
	fallback, err := c.Fallback(o)
	if err != nil {
		return nil, err
	}
	file := ""
	if c.Path() != "" {
		file = credentials.FilePath(c.Path())
	}
	return credentials.New(credentials.Options{
		Policy:      credentials.Policy(fallback),
		Service:     credentials.Service,
		File:        file,
		Interactive: interactive,
	})
}
