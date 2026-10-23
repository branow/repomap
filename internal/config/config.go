// Package config owns config.yml. No struct here has a secret field: an access
// token lives in the keychain, reached only through internal/credentials.
package config

import "os"

// Forge names a supported source of repositories.
type Forge string

const (
	Bitbucket Forge = "bitbucket"
	GitHub    Forge = "github"
	// Local is a directory of checkouts already on this machine. It has no API
	// and no credential, which is what makes the tool runnable by someone who
	// has not been given access to anything yet.
	Local Forge = "local"
)

// Auth names how a workspace authenticates to its forge.
type Auth string

const (
	// Token is an app password or personal access token, kept in the keychain.
	Token Auth = "token"
	// None covers a public forge and a local directory alike: nothing to store.
	None Auth = "none"
)

// Provider names an llm backend implementation.
type Provider string

const (
	Anthropic  Provider = "anthropic"
	OpenAI     Provider = "openai"
	ClaudeCode Provider = "claudecode"
)

// Fallback is the secret-storage policy when the OS keychain cannot answer.
// Never is the default: plaintext is opt-in only.
type Fallback string

const (
	Never     Fallback = "never"
	Plaintext Fallback = "plaintext"
)

// Workspace is one named set of repositories. Its token, when the auth mode
// needs one, lives in the keychain under forge:<name>.
type Workspace struct {
	Forge Forge `yaml:"forge"`
	// Slug is the account the forge lists repositories under: a Bitbucket
	// workspace, a GitHub organisation or user.
	Slug string `yaml:"slug,omitempty"`
	// BaseURL points at a self-hosted forge; empty means the public one.
	BaseURL string `yaml:"base_url,omitempty"`
	Auth    Auth   `yaml:"auth"`
	// Username is the account the token belongs to. Bitbucket app passwords are
	// presented as basic auth and need it; a GitHub token does not.
	Username string `yaml:"username,omitempty"`
	// Dir is where the local forge finds checkouts, and is meaningless for the
	// others: a directory this tool reads and never writes.
	Dir string `yaml:"dir,omitempty"`
	// Clones is where this workspace's clones are kept, defaulting to a
	// per-workspace directory under the user's cache. Set it to point the tool
	// at a checkout tree something else already maintains.
	Clones string `yaml:"clones,omitempty"`
	// Exclude drops repositories whose name contains any of these substrings,
	// or exactly matches one written with a leading "=", so an account full of
	// archived forks can be indexed without them.
	Exclude []string `yaml:"exclude,omitempty"`
}

// Backend is one named llm backend. Its api key, when the provider needs one,
// lives in the keychain under llm:<name>.
type Backend struct {
	Provider Provider `yaml:"provider"`
	Model    string   `yaml:"model,omitempty"`
	BaseURL  string   `yaml:"base_url,omitempty"`
}

// Profile binds one workspace to one backend; it never redefines either.
type Profile struct {
	Workspace string `yaml:"workspace"`
	Backend   string `yaml:"backend"`
	Output    string `yaml:"output,omitempty"`
}

// Secrets holds the secret-storage policy.
type Secrets struct {
	Fallback Fallback `yaml:"fallback,omitempty"`
}

// Config is the whole file plus the precedence rules that read it.
type Config struct {
	CurrentProfile string               `yaml:"current_profile,omitempty"`
	DefaultOutput  string               `yaml:"output,omitempty"`
	Secrets        Secrets              `yaml:"secrets,omitempty"`
	Workspaces     map[string]Workspace `yaml:"workspaces,omitempty"`
	Backends       map[string]Backend   `yaml:"backends,omitempty"`
	Profiles       map[string]Profile   `yaml:"profiles,omitempty"`

	path string
	env  func(string) string
}

// New returns an empty config with no file behind it; Save fails until a path
// is given.
func New() *Config {
	return &Config{
		Workspaces: map[string]Workspace{},
		Backends:   map[string]Backend{},
		Profiles:   map[string]Profile{},
		env:        os.Getenv,
	}
}

// NewAt returns an empty config bound to a file path.
func NewAt(path string) *Config {
	c := New()
	c.path = path
	return c
}

// Path reports the file this config loads from and saves to.
func (c *Config) Path() string { return c.path }

// SetPath binds the config to a file.
func (c *Config) SetPath(p string) { c.path = p }

// SetEnv replaces the environment lookup, so precedence is testable without
// mutating the process.
func (c *Config) SetEnv(lookup func(string) string) { c.env = lookup }

func (c *Config) lookup(name string) string {
	if c.env == nil {
		return ""
	}
	return c.env(name)
}
