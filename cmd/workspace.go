package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/branow/repomap/internal/cmdutil"
	"github.com/branow/repomap/internal/config"
	"github.com/branow/repomap/internal/credentials"
	"github.com/branow/repomap/internal/output"
)

// newWorkspace builds the workspace namespace. Defining a workspace never
// activates it: `repomap profile switch` does that.
func newWorkspace(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage named sets of repositories",
		Long: "A workspace holds non-secret settings only. Its access token lives " +
			"in the OS keychain under forge:<name> and never enters the config " +
			"file.\n\n" +
			"The local forge needs no token and no network: it indexes the " +
			"checkouts already in a directory, which is the quickest way to see " +
			"what this tool produces.",
	}
	cmd.AddCommand(
		newWorkspaceAdd(f),
		newWorkspaceList(f),
		newWorkspaceShow(f),
		newWorkspaceRemove(f),
	)
	return cmd
}

// workspaceOptions is the flag surface of `workspace add`. Every value is a
// flag so a CI run needs no terminal; only the token is ever prompted for.
type workspaceOptions struct {
	forge    string
	slug     string
	baseURL  string
	dir      string
	clones   string
	auth     string
	username string
	exclude  []string
	stdin    bool
	noVerify bool
}

func newWorkspaceAdd(f *cmdutil.Factory) *cobra.Command {
	opts := workspaceOptions{}
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Define a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return addWorkspace(c, f, args[0], &opts)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.forge, "forge", "", "forge: "+strings.Join(config.Forges(), ", "))
	flags.StringVar(&opts.slug, "slug", "", "the account repositories are listed under")
	flags.StringVar(&opts.baseURL, "base-url", "", "api root of a self-hosted forge")
	flags.StringVar(&opts.dir, "dir", "", "directory of checkouts, for the local forge")
	flags.StringVar(&opts.clones, "clones", "",
		"where clones are kept; defaults to a directory under the user cache")
	flags.StringVar(&opts.auth, "auth", "", "authentication mode")
	flags.StringVar(&opts.username, "username", "", "login name the token belongs to")
	flags.StringArrayVar(&opts.exclude, "exclude", nil,
		"skip repositories whose name contains this, repeatable")
	flags.BoolVar(&opts.stdin, "token-stdin", false, "read the access token from stdin")
	flags.BoolVar(&opts.noVerify, "no-verify", false, "store without probing the forge")
	return cmd
}

// addWorkspace collects the settings, verifies before it stores anything, then
// writes the token to the keychain and the rest to the config file.
func addWorkspace(c *cobra.Command, f *cmdutil.Factory, name string,
	opts *workspaceOptions) error {

	if err := require(&opts.forge, "--forge", string(config.Bitbucket), true); err != nil {
		return err
	}
	forgeName, err := config.ParseForge(opts.forge)
	if err != nil {
		return err
	}
	if config.NeedsDir(forgeName) {
		if err := require(&opts.dir, "--dir", "", true); err != nil {
			return err
		}
	} else if err := require(&opts.slug, "--slug", "", true); err != nil {
		return err
	}

	allowed := config.Auths(forgeName)
	if err := require(&opts.auth, "--auth", allowed[0], true); err != nil {
		return err
	}
	auth, err := config.ParseAuth(forgeName, opts.auth)
	if err != nil {
		return err
	}
	if config.NeedsUsername(forgeName, auth) {
		if err := require(&opts.username, "--username", "", true); err != nil {
			return err
		}
	}

	entry := config.Workspace{
		Forge:    forgeName,
		Slug:     opts.slug,
		BaseURL:  opts.baseURL,
		Dir:      opts.dir,
		Clones:   opts.clones,
		Auth:     auth,
		Username: opts.username,
		Exclude:  opts.exclude,
	}
	key := credentials.ForgeKey(name)
	secret, from, err := readSecret(f, opts.stdin, key, "access token", "--token-stdin",
		config.NeedsToken(auth))
	if err != nil {
		return err
	}
	var probe func() error
	if f.Probes.Workspace != nil {
		probe = func() error { return f.Probes.Workspace(c.Context(), name, entry, secret) }
	}
	if err := verify(f, opts.noVerify, probe); err != nil {
		return err
	}
	if err := remember(f, key, secret, from); err != nil {
		return err
	}
	if err := f.Config.SetWorkspace(name, entry); err != nil {
		return err
	}
	if err := f.Config.Save(); err != nil {
		return err
	}
	return f.Note("workspace " + name + " defined; activate it with a profile")
}

func newWorkspaceList(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List defined workspaces",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			records := []output.Record{}
			for _, name := range f.Config.WorkspaceNames() {
				entry := f.Config.Workspaces[name]
				records = append(records, output.Record{
					{Name: "name", Value: name},
					{Name: "forge", Value: string(entry.Forge)},
					{Name: "account", Value: account(entry)},
					{Name: "auth", Value: string(entry.Auth)},
				})
			}
			return f.Writer().List(records)
		},
	}
}

func newWorkspaceShow(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show one workspace's settings",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			entry, err := f.Config.Workspace(args[0])
			if err != nil {
				return err
			}
			return f.Writer().Show(output.Record{
				{Name: "name", Value: args[0]},
				{Name: "forge", Value: string(entry.Forge)},
				{Name: "account", Value: account(entry)},
				{Name: "base url", Value: entry.BaseURL},
				{Name: "auth", Value: string(entry.Auth)},
				{Name: "username", Value: entry.Username},
				{Name: "clones", Value: entry.Clones},
				{Name: "exclude", Value: entry.Exclude},
			})
		},
	}
}

func newWorkspaceRemove(f *cmdutil.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a workspace and its stored token",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			if err := f.Config.RemoveWorkspace(name, f.Flags.Force); err != nil {
				return err
			}
			if err := forget(f, credentials.ForgeKey(name)); err != nil {
				return err
			}
			if err := f.Config.Save(); err != nil {
				return err
			}
			// The clones are deliberately left where they are. They may be a
			// tree the user maintains for something else, and a command that
			// silently deleted gigabytes of checkouts on a config edit would be
			// a command nobody runs twice.
			return f.Note("workspace " + name + " removed; its clones are untouched")
		},
	}
}

// account renders what a workspace is pointed at: a forge account, or the
// directory the local forge reads.
func account(entry config.Workspace) string {
	if entry.Slug != "" {
		return entry.Slug
	}
	return entry.Dir
}
