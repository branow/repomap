package config

import (
	"regexp"
	"sort"
)

// forges lists what each forge takes. A forge that lists repositories over an
// API needs a slug to list them under and can carry a token; the local forge
// reads a directory and can carry neither.
var forges = map[Forge]struct {
	Auths    []Auth
	NeedsDir bool
	// Remote marks a forge reached over the network, and so one whose
	// repositories are cloned rather than read where they already are.
	Remote bool
}{
	Bitbucket: {Auths: []Auth{Token, None}, Remote: true},
	GitHub:    {Auths: []Auth{Token, None}, Remote: true},
	Local:     {Auths: []Auth{None}, NeedsDir: true},
}

// providers lists the llm providers and what each one takes. ClaudeCode runs a
// local binary, so it has neither a key nor an endpoint.
var providers = map[Provider]struct{ NeedsKey, HasEndpoint bool }{
	Anthropic:  {NeedsKey: true, HasEndpoint: true},
	OpenAI:     {NeedsKey: true, HasEndpoint: true},
	ClaudeCode: {NeedsKey: false, HasEndpoint: false},
}

var fallbacks = []Fallback{Never, Plaintext}

// secretless lists the auth modes where nothing has to be stored.
var secretless = map[Auth]bool{None: true}

// name is the shape of every entry name: safe to type, and safe in a keychain
// key.
var name = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Forges lists the supported forges.
func Forges() []string {
	out := make([]string, 0, len(forges))
	for f := range forges {
		out = append(out, string(f))
	}
	sort.Strings(out)
	return out
}

// Providers lists the supported llm providers.
func Providers() []string {
	out := make([]string, 0, len(providers))
	for p := range providers {
		out = append(out, string(p))
	}
	sort.Strings(out)
	return out
}

// Fallbacks lists the secret-storage policies.
func Fallbacks() []string {
	out := make([]string, len(fallbacks))
	for i, f := range fallbacks {
		out[i] = string(f)
	}
	return out
}

// Auths lists the authentication modes a forge accepts.
func Auths(f Forge) []string {
	modes := forges[f].Auths
	out := make([]string, len(modes))
	for i, m := range modes {
		out[i] = string(m)
	}
	return out
}

// NeedsToken reports whether an auth mode requires a stored token.
func NeedsToken(a Auth) bool { return !secretless[a] }

// NeedsUsername reports whether a forge presents its token with a login name.
// Bitbucket app passwords are basic auth; a GitHub token stands alone.
func NeedsUsername(f Forge, a Auth) bool { return f == Bitbucket && NeedsToken(a) }

// NeedsDir reports whether a forge reads a directory instead of an API.
func NeedsDir(f Forge) bool { return forges[f].NeedsDir }

// Remote reports whether a forge is reached over the network, and so whether
// its repositories have to be cloned before they can be read.
func Remote(f Forge) bool { return forges[f].Remote }

// NeedsAPIKey reports whether a provider requires a stored api key.
func NeedsAPIKey(p Provider) bool { return providers[p].NeedsKey }

// UsesBaseURL reports whether a provider has an endpoint at all.
func UsesBaseURL(p Provider) bool { return providers[p].HasEndpoint }

// Validate checks every entry is well formed and every profile binds names
// that exist.
func (c *Config) Validate() error {
	for entry, value := range c.Workspaces {
		if err := validateWorkspace(entry, value); err != nil {
			return err
		}
	}
	for entry, value := range c.Backends {
		if err := validateBackend(entry, value); err != nil {
			return err
		}
	}
	// A profile pointing at an entry that no longer exists is not a reason to
	// refuse the whole file: that would make one removal brick every command,
	// including the ones that would repair it. A dangling binding fails when the
	// profile is actually used, and nowhere else.
	for entry := range c.Profiles {
		if err := validateName(KindProfile, entry); err != nil {
			return err
		}
	}
	if c.Secrets.Fallback != "" {
		if err := validateFallback(string(c.Secrets.Fallback)); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkspace(entry string, value Workspace) error {
	if err := validateName(KindWorkspace, entry); err != nil {
		return err
	}
	spec, ok := forges[value.Forge]
	if !ok {
		return &InvalidError{Field: "forge", Value: string(value.Forge), Allowed: Forges()}
	}
	if spec.NeedsDir && value.Dir == "" {
		return &InvalidError{Field: "dir", Value: "",
			Reason: "the " + string(value.Forge) + " forge reads a directory of checkouts"}
	}
	if !spec.NeedsDir && value.Slug == "" {
		return &InvalidError{Field: "slug", Value: "",
			Reason: "must name the account the forge lists repositories under"}
	}
	for _, mode := range spec.Auths {
		if mode == value.Auth {
			return nil
		}
	}
	return &InvalidError{Field: "auth", Value: string(value.Auth), Allowed: Auths(value.Forge)}
}

func validateBackend(entry string, value Backend) error {
	if err := validateName(KindBackend, entry); err != nil {
		return err
	}
	if _, ok := providers[value.Provider]; !ok {
		return &InvalidError{Field: "provider", Value: string(value.Provider),
			Allowed: Providers()}
	}
	return nil
}

func validateName(kind Kind, entry string) error {
	if name.MatchString(entry) {
		return nil
	}
	return &InvalidError{Field: string(kind) + " name", Value: entry,
		Reason: "must start with a letter or digit and hold only letters, digits, dot, dash " +
			"or underscore"}
}

func validateFallback(value string) error {
	for _, f := range fallbacks {
		if string(f) == value {
			return nil
		}
	}
	return &InvalidError{Field: "secrets.fallback", Value: value, Allowed: Fallbacks()}
}

// ParseForge validates a forge name coming from a flag or a prompt.
func ParseForge(value string) (Forge, error) {
	if _, ok := forges[Forge(value)]; ok {
		return Forge(value), nil
	}
	return "", &InvalidError{Field: "forge", Value: value, Allowed: Forges()}
}

// ParseAuth validates an auth mode against the forge that must accept it.
func ParseAuth(f Forge, value string) (Auth, error) {
	for _, mode := range forges[f].Auths {
		if string(mode) == value {
			return mode, nil
		}
	}
	return "", &InvalidError{Field: "auth", Value: value, Allowed: Auths(f)}
}

// ParseProvider validates an llm provider name.
func ParseProvider(value string) (Provider, error) {
	if _, ok := providers[Provider(value)]; ok {
		return Provider(value), nil
	}
	return "", &InvalidError{Field: "provider", Value: value, Allowed: Providers()}
}
