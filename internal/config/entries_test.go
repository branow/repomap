package config

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestDefiningNeverActivates(t *testing.T) {
	c := New()
	if err := c.SetWorkspace("primary", Workspace{Forge: Bitbucket,
		Slug: "example", Auth: Token, Username: "reader"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBackend("main", Backend{Provider: Anthropic}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("work", Profile{Workspace: "primary", Backend: "main"}); err != nil {
		t.Fatal(err)
	}
	if c.CurrentProfile != "" {
		t.Fatalf("current profile = %q after defining entries, want none", c.CurrentProfile)
	}
	if err := c.Switch("work"); err != nil {
		t.Fatal(err)
	}
	if c.CurrentProfile != "work" {
		t.Errorf("current profile = %q after switch, want work", c.CurrentProfile)
	}
}

func TestSwitchRefusesAnUndefinedProfile(t *testing.T) {
	c := New()
	var missing *NotFoundError
	if err := c.Switch("nope"); !errors.As(err, &missing) || missing.Kind != KindProfile {
		t.Fatalf("error = %v, want a profile NotFoundError", err)
	}
}

func TestProfileMustBindDefinedEntries(t *testing.T) {
	c := New()
	if err := c.SetWorkspace("primary", Workspace{Forge: GitHub,
		Slug: "example", Auth: Token}); err != nil {
		t.Fatal(err)
	}
	var missing *NotFoundError
	err := c.SetProfile("work", Profile{Workspace: "primary", Backend: "absent"})
	if !errors.As(err, &missing) || missing.Kind != KindBackend {
		t.Fatalf("error = %v, want a backend NotFoundError", err)
	}
}

func TestRemoveRefusesWhileBound(t *testing.T) {
	c := loaded(t)
	var inUse *InUseError
	if err := c.RemoveWorkspace("primary", false); !errors.As(err, &inUse) {
		t.Fatalf("error = %v, want InUseError", err)
	}
	if err := c.RemoveWorkspace("primary", true); err != nil {
		t.Fatalf("forced removal: %v", err)
	}
	if _, err := c.Workspace("primary"); err == nil {
		t.Error("workspace survived a forced removal")
	}
}

func TestRemoveProfileClearsCurrent(t *testing.T) {
	c := loaded(t)
	if err := c.Switch("work"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveProfile("work"); err != nil {
		t.Fatal(err)
	}
	if c.CurrentProfile != "" {
		t.Errorf("current profile = %q after removing it", c.CurrentProfile)
	}
}

func TestInvalidEntries(t *testing.T) {
	tests := []struct {
		name  string
		field string
		apply func(*Config) error
	}{
		{name: "unknown forge", field: "forge", apply: func(c *Config) error {
			return c.SetWorkspace("primary", Workspace{Forge: "gitea",
				Slug: "example", Auth: Token})
		}},
		{name: "auth the forge rejects", field: "auth", apply: func(c *Config) error {
			return c.SetWorkspace("primary", Workspace{Forge: Local,
				Dir: "/checkouts", Auth: Token})
		}},
		{name: "remote forge with no slug", field: "slug", apply: func(c *Config) error {
			return c.SetWorkspace("primary", Workspace{Forge: Bitbucket, Auth: None})
		}},
		{name: "local forge with no directory", field: "dir", apply: func(c *Config) error {
			return c.SetWorkspace("primary", Workspace{Forge: Local, Auth: None})
		}},
		{name: "unknown provider", field: "provider", apply: func(c *Config) error {
			return c.SetBackend("main", Backend{Provider: "acme"})
		}},
		{name: "name with a slash", field: "workspace name", apply: func(c *Config) error {
			return c.SetWorkspace("a/b", Workspace{Forge: Bitbucket,
				Slug: "example", Auth: None})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var invalid *InvalidError
			err := tt.apply(New())
			if !errors.As(err, &invalid) {
				t.Fatalf("error = %v, want InvalidError", err)
			}
			if invalid.Field != tt.field {
				t.Errorf("field = %q, want %q", invalid.Field, tt.field)
			}
		})
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	c := NewAt(path)
	if err := c.SetWorkspace("primary", Workspace{Forge: Bitbucket,
		Slug: "example", BaseURL: "https://git.example.internal", Auth: Token,
		Username: "reader", Exclude: []string{"-archive"},
		Deprecated: []string{"legacy"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBackend("main", Backend{Provider: OpenAI, Model: "model-a",
		BaseURL: "https://api.example.internal"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("work", Profile{Workspace: "primary", Backend: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Switch("work"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := reloaded.Workspace("primary")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Slug != "example" || entry.BaseURL != "https://git.example.internal" {
		t.Errorf("workspace round trip lost fields: %+v", entry)
	}
	if len(entry.Exclude) != 1 || entry.Exclude[0] != "-archive" {
		t.Errorf("exclude round trip lost fields: %+v", entry.Exclude)
	}
	if len(entry.Deprecated) != 1 || entry.Deprecated[0] != "legacy" {
		t.Errorf("deprecated round trip lost fields: %+v", entry.Deprecated)
	}
	if reloaded.CurrentProfile != "work" {
		t.Errorf("current profile = %q", reloaded.CurrentProfile)
	}
}

func TestSavedFileIsOwnerOnlyAndHoldsNoSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	c := NewAt(path)
	if err := c.SetWorkspace("primary", Workspace{Forge: Bitbucket,
		Slug: "example", Auth: Token, Username: "reader"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	// Windows reports 0666 for any writable file whatever mode was passed, so
	// the mode check cannot run there; the ACL on the profile directory is what
	// protects it. The content check below runs everywhere, and on Windows it is
	// the whole of this test.
	if runtime.GOOS != "windows" {
		info, err := stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("permissions = %o, want 600", perm)
		}
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "username: reader") {
		t.Errorf("the workspace was not saved:\n%s", raw)
	}
	// No secret field exists on any config struct, so this fails only if one is
	// added - which is the mistake worth catching. `secrets:` is allowed: that
	// stanza holds the storage POLICY, never a value.
	if secretish.MatchString(string(raw)) {
		t.Errorf("a credential-shaped key reached the config file:\n%s", raw)
	}
}

// secretish matches the keys that would mean a secret had been given a home in
// the file. Deliberately not `secret`, which would hit the policy stanza.
var secretish = regexp.MustCompile(`(?i)\b(password|passwd|pwd|api[_-]?key|apikey|` +
	`access[_-]?token|client[_-]?secret)\b`)

func TestLoadOfAMissingFileIsEmpty(t *testing.T) {
	c, err := LoadFrom(filepath.Join(t.TempDir(), "absent.yml"))
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(c.Workspaces) != 0 || len(c.Profiles) != 0 {
		t.Error("a missing file produced entries")
	}
}

func TestLoadRejectsAnInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := write(path, "workspaces:\n  primary:\n    forge: gitea\n    slug: s\n"); err != nil {
		t.Fatal(err)
	}
	var invalid *InvalidError
	if _, err := LoadFrom(path); !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want InvalidError", err)
	}
}

func loaded(t *testing.T) *Config {
	t.Helper()
	c := NewAt(filepath.Join(t.TempDir(), "config.yml"))
	if err := c.SetWorkspace("primary", Workspace{Forge: Bitbucket,
		Slug: "example", Auth: Token, Username: "reader"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetBackend("main", Backend{Provider: Anthropic}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("work", Profile{Workspace: "primary", Backend: "main"}); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestForgeAuthPairings pins the auth table: both remote forges take a token or
// nothing, and the local forge takes only nothing - there is no endpoint for a
// token to be presented to.
func TestForgeAuthPairings(t *testing.T) {
	tests := []struct {
		forge Forge
		auth  Auth
		want  bool
	}{
		{forge: Bitbucket, auth: Token, want: true},
		{forge: Bitbucket, auth: None, want: true},
		{forge: GitHub, auth: Token, want: true},
		{forge: GitHub, auth: None, want: true},
		{forge: Local, auth: None, want: true},
		{forge: Local, auth: Token, want: false},
	}
	for _, tt := range tests {
		t.Run(string(tt.forge)+"/"+string(tt.auth), func(t *testing.T) {
			_, err := ParseAuth(tt.forge, string(tt.auth))
			if (err == nil) != tt.want {
				t.Fatalf("ParseAuth = %v, want accepted = %v", err, tt.want)
			}
			entry := Workspace{Forge: tt.forge, Slug: "example", Dir: "/checkouts", Auth: tt.auth}
			if err := New().SetWorkspace("primary", entry); (err == nil) != tt.want {
				t.Errorf("SetWorkspace = %v, want accepted = %v", err, tt.want)
			}
		})
	}
}

// TestPublicForgeNeedsNoStoredCredential: a public account is readable without
// one, so nothing is demanded and nothing is stored.
func TestPublicForgeNeedsNoStoredCredential(t *testing.T) {
	if NeedsToken(None) {
		t.Error("an unauthenticated workspace is being asked for a token")
	}
	if !NeedsToken(Token) {
		t.Error("a token workspace is not being asked for a token")
	}
	// A Bitbucket app password is presented as basic auth and so carries a login
	// name; a GitHub token stands alone. That difference is the reason the
	// question is asked of the forge and not of the auth mode alone.
	if !NeedsUsername(Bitbucket, Token) {
		t.Error("a bitbucket token is not being asked for a login name")
	}
	if NeedsUsername(GitHub, Token) || NeedsUsername(Local, None) {
		t.Error("a login name is being demanded where the forge does not use one")
	}
}

// TestLocalForgeIsNotCloned pins what separates the local forge from the others:
// it reads checkouts where they already are.
func TestLocalForgeIsNotCloned(t *testing.T) {
	if Remote(Local) || !NeedsDir(Local) {
		t.Error("the local forge is being treated as a remote one")
	}
	for _, f := range []Forge{Bitbucket, GitHub} {
		if !Remote(f) || NeedsDir(f) {
			t.Errorf("%s is being treated as a local directory", f)
		}
	}
}
