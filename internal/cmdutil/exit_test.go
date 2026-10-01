package cmdutil

import (
	"errors"
	"fmt"
	"testing"

	"github.com/branow/gollm"
	"github.com/branow/repomap/internal/clone"
	"github.com/branow/repomap/internal/config"
	"github.com/branow/repomap/internal/credentials"
	"github.com/branow/repomap/internal/forge"
	"github.com/branow/repomap/internal/forge/local"
	"github.com/branow/repomap/internal/git"
	"github.com/branow/repomap/internal/iostreams"
	"github.com/branow/repomap/internal/output"
	"github.com/branow/repomap/internal/scan"
)

// TestExitCode covers every class in the documented table, and covers each one
// through a wrapped error too: the mapping is errors.Is and errors.As, so a
// wrapped cause must still decide the status.
func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: ExitOK},
		{name: "unclassified", err: errors.New("something broke"), want: ExitError},
		{name: "cancelled", err: ErrCancelled, want: ExitCancelled},
		{name: "input ended at a prompt", err: iostreams.ErrCancelled, want: ExitCancelled},
		{name: "prompting disabled", err: iostreams.ErrNoInput, want: ExitValidation},
		{name: "validation", err: &ValidationError{Field: "--slug", Reason: "required"},
			want: ExitValidation},
		{name: "invalid config value",
			err:  &config.InvalidError{Field: "forge", Value: "gitea"},
			want: ExitValidation},
		{name: "entry still in use",
			err:  &config.InUseError{Kind: config.KindWorkspace, Name: "primary"},
			want: ExitValidation},
		{name: "unknown output format",
			err:  &output.UnknownFormatError{Value: "tsv"},
			want: ExitValidation},
		{name: "unsupported forge",
			err:  &forge.UnsupportedError{Forge: "gitea", Supported: []string{"github"}},
			want: ExitValidation},
		{name: "unreadable checkout directory",
			err:  &local.DirectoryError{Dir: "/checkouts", Err: errors.New("no such file")},
			want: ExitValidation},
		{name: "auth", err: &AuthError{Subject: "primary"}, want: ExitAuth},
		{name: "forge refused the token",
			err:  &forge.AuthError{Forge: "bitbucket", Slug: "example", Status: 401},
			want: ExitAuth},
		{name: "model rejected the key",
			err:  &gollm.Error{Class: gollm.ClassAuth},
			want: ExitAuth},
		{name: "not configured", err: &NotConfiguredError{What: "current profile"},
			want: ExitNotFound},
		{name: "unknown entry",
			err:  &config.NotFoundError{Kind: config.KindProfile, Name: "work"},
			want: ExitNotFound},
		{name: "no stored secret", err: &credentials.NotFoundError{Key: "forge:primary"},
			want: ExitNotFound},
		{name: "no such account",
			err:  &forge.NotFoundError{Forge: "github", Slug: "typo"},
			want: ExitNotFound},
		{name: "repository with no commits",
			err:  &scan.EmptyError{Dir: "/clones/new-repo"},
			want: ExitNotFound},
		{name: "unavailable", err: &UnavailableError{Subject: "the forge"},
			want: ExitUnavailable},
		{name: "keychain", err: &credentials.KeychainError{Op: "read", Key: "forge:primary"},
			want: ExitUnavailable},
		{name: "read-only store", err: &credentials.ReadOnlyError{Store: "environment"},
			want: ExitUnavailable},
		{name: "forge is down",
			err:  &forge.UnavailableError{Forge: "bitbucket", Status: 503},
			want: ExitUnavailable},
		{name: "forge answered unreadably",
			err:  &forge.ResponseError{Forge: "github", URL: "https://example.internal"},
			want: ExitUnavailable},
		{name: "remote unreachable",
			err:  &clone.AccessError{Remote: "git@example.internal:acme/api.git"},
			want: ExitUnavailable},
		{name: "clone failed",
			err:  &clone.CloneError{Repo: "api", Remote: "git@example.internal:acme/api.git", Err: errors.New("x")},
			want: ExitUnavailable},
		{name: "update failed",
			err:  &clone.UpdateError{Repo: "api", Dir: "/clones/api", Err: errors.New("x")},
			want: ExitUnavailable},
		{name: "git is not installed", err: &git.MissingError{}, want: ExitUnavailable},
		{name: "model rate limited",
			err:  &gollm.Error{Class: gollm.ClassRateLimited},
			want: ExitUnavailable},
		{name: "model unavailable",
			err:  &gollm.Error{Class: gollm.ClassUnavailable},
			want: ExitUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
			if tt.err == nil {
				return
			}
			wrapped := fmt.Errorf("while adding a workspace: %w", tt.err)
			if got := ExitCode(wrapped); got != tt.want {
				t.Errorf("ExitCode(wrapped %v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "validation with a reason",
			err:  &ValidationError{Field: "--slug", Reason: "required"},
			want: `invalid --slug: required`},
		{name: "validation with a value",
			err:  &ValidationError{Field: "shell", Value: "csh", Allowed: []string{"bash", "zsh"}},
			want: `invalid shell "csh", want one of bash, zsh`},
		{name: "not configured names the fix",
			err:  &NotConfiguredError{What: "current profile", Fix: "run repomap profile switch"},
			want: "current profile is not configured: run repomap profile switch"},
		{name: "auth", err: &AuthError{Subject: "backend main"},
			want: "authentication failed for backend main"},
		{name: "unavailable", err: &UnavailableError{Subject: "keychain"},
			want: "keychain is unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("message = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTheCauseDecidesNotTheWrapper pins the ordering of the rule table. A clone
// failure carries what actually went wrong, and the cause is the more
// actionable of the two: a refused credential inside one is an auth problem
// that waiting will not fix, not a forge that happens to be down.
func TestTheCauseDecidesNotTheWrapper(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "a refused token inside a clone failure",
			err: &clone.CloneError{Repo: "api", Remote: "git@example.internal:acme/api.git",
				Err: &forge.AuthError{Forge: "bitbucket", Status: 401}},
			want: ExitAuth},
		{name: "a missing git inside a clone failure",
			err: &clone.CloneError{Repo: "api", Remote: "git@example.internal:acme/api.git",
				Err: &git.MissingError{}},
			want: ExitUnavailable},
		{name: "a clone failure with an unclassified cause stays unavailable",
			err: &clone.CloneError{Repo: "api", Remote: "git@example.internal:acme/api.git",
				Err: errors.New("fatal: could not read from remote repository")},
			want: ExitUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestEveryDocumentedCodeIsReachable guards against a code that is documented
// and never produced, and against an error type with no home: one that maps
// nowhere falls into the generic bucket silently.
func TestEveryDocumentedCodeIsReachable(t *testing.T) {
	reached := map[int]bool{
		ExitOK:    ExitCode(nil) == ExitOK,
		ExitError: ExitCode(errors.New("unclassified")) == ExitError,
	}
	for _, rule := range rules {
		reached[rule.code] = true
	}
	for _, code := range Codes() {
		if !reached[code.Value] {
			t.Errorf("exit code %d (%s) is documented but nothing produces it",
				code.Value, code.Name)
		}
		if code.Meaning == "" {
			t.Errorf("exit code %d (%s) is undocumented", code.Value, code.Name)
		}
	}
}

func TestEveryRuleUsesADocumentedCode(t *testing.T) {
	documented := map[int]bool{}
	for _, code := range Codes() {
		documented[code.Value] = true
	}
	for _, rule := range rules {
		if !documented[rule.code] {
			t.Errorf("rule maps to exit code %d, which the table does not document", rule.code)
		}
	}
}
