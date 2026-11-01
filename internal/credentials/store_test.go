package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/branow/gokey"
)

// stub is a keychain whose three operations are scripted, so every branch is
// reachable without an operating system credential store being present.
func stub(values map[string]string, failure error) *Keychain {
	return script(NewKeychain("test"), values, failure)
}

// script fills in the seams of an already built keychain, so a test can choose
// how the keychain was constructed and still drive it from memory.
func script(k *Keychain, values map[string]string, failure error) *Keychain {
	k.get = func(_, key string, _ bool) (string, error) {
		if failure != nil {
			return "", failure
		}
		value, ok := values[key]
		if !ok {
			return "", gokey.ErrNotFound
		}
		return value, nil
	}
	k.set = func(_, key, value string, _ bool) error {
		if failure != nil {
			return failure
		}
		values[key] = value
		return nil
	}
	k.remove = func(_, key string, _ bool) error {
		if failure != nil {
			return failure
		}
		if _, ok := values[key]; !ok {
			return gokey.ErrNotFound
		}
		delete(values, key)
		return nil
	}
	// Never the real /usr/bin/security from a test.
	k.dropForeign = func(_, _ string) error {
		return errors.New("the scripted keychain owns every item")
	}
	return k
}

// call is one operation as it reached a seam: which one, and whether it was
// allowed to raise the dialog.
type call struct {
	op     string
	prompt bool
}

// spy records the prompt choice every operation is made with. Whether a dialog
// was refused is invisible in the result of a call, so the test asserts on what
// reached the seam instead.
func spy(k *Keychain) *[]call {
	seen := &[]call{}
	get, set, remove := k.get, k.set, k.remove
	k.get = func(service, key string, prompt bool) (string, error) {
		*seen = append(*seen, call{"get", prompt})
		return get(service, key, prompt)
	}
	k.set = func(service, key, value string, prompt bool) error {
		*seen = append(*seen, call{"set", prompt})
		return set(service, key, value, prompt)
	}
	k.remove = func(service, key string, prompt bool) error {
		*seen = append(*seen, call{"remove", prompt})
		return remove(service, key, prompt)
	}
	return seen
}

var broken = errors.New("dbus is not running")

func TestKeychainErrors(t *testing.T) {
	t.Run("missing key", func(t *testing.T) {
		_, err := stub(map[string]string{}, nil).Get(ForgeKey("primary"))
		var missing *NotFoundError
		if !errors.As(err, &missing) {
			t.Fatalf("error = %v, want NotFoundError", err)
		}
	})
	t.Run("unavailable keychain names the fix", func(t *testing.T) {
		_, err := stub(nil, broken).Get(ForgeKey("primary"))
		var failure *KeychainError
		if !errors.As(err, &failure) {
			t.Fatalf("error = %v, want KeychainError", err)
		}
		if failure.Remedy == "" {
			t.Error("the error does not name a way out")
		}
		if !errors.Is(err, broken) {
			t.Error("the cause was dropped")
		}
	})
	t.Run("deleting an absent key is not an error", func(t *testing.T) {
		if err := stub(map[string]string{}, nil).Delete(ForgeKey("primary")); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	})
}

// TestKeychainAsksOnlyWhenSomeoneIsWatching is the hang this prevents: a store
// not told a human is present must refuse the keychain's dialog on every
// operation, because a dialog nobody can see blocks forever. A read is silent
// first even when someone is watching: an item this build stored needs no
// dialog, and only a refusal earns one.
func TestKeychainAsksOnlyWhenSomeoneIsWatching(t *testing.T) {
	tests := []struct {
		name     string
		keychain *Keychain
		want     []call
	}{
		{name: "default", keychain: NewKeychain("test"),
			want: []call{{"get", false}, {"set", false}, {"remove", false}}},
		{name: "non-interactive", keychain: newKeychain("test", false, nil),
			want: []call{{"get", false}, {"set", false}, {"remove", false}}},
		{name: "interactive", keychain: newKeychain("test", true, nil),
			want: []call{{"get", false}, {"set", true}, {"remove", true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := ForgeKey("primary")
			keychain := script(tt.keychain, map[string]string{key: password}, nil)
			seen := spy(keychain)
			if _, err := keychain.Get(key); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if err := keychain.Set(key, NewSecret(password)); err != nil {
				t.Fatalf("Set: %v", err)
			}
			if err := keychain.Delete(key); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if len(*seen) != len(tt.want) {
				t.Fatalf("calls = %v, want %v", *seen, tt.want)
			}
			for i, got := range *seen {
				if got != tt.want[i] {
					t.Errorf("call %d = %v, want %v", i, got, tt.want[i])
				}
			}
		})
	}
}

// TestBlockedKeychainNamesBothWaysOut pins the message a user sees after a
// rebuild: "keychain unavailable" alone leaves them nothing to do.
func TestBlockedKeychainNamesBothWaysOut(t *testing.T) {
	key := ForgeKey("primary")
	_, err := stub(nil, gokey.ErrBlocked).Get(key)

	var failure *KeychainError
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want KeychainError", err)
	}
	if !errors.Is(err, gokey.ErrBlocked) {
		t.Error("the cause was dropped")
	}
	for _, want := range []string{"re-authorize", "re-add", EnvName(key)} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
	if strings.Contains(failure.Remedy, "--allow-plaintext") {
		t.Error("a blocked read offers the plaintext file instead of the way out")
	}
}

// TestUnavailableKeychainDoesNotOfferToReauthorize separates the two refusals:
// there is no credential store to re-authorize against on a headless box, so
// the only remedies worth printing are the ones that need no keychain.
func TestUnavailableKeychainDoesNotOfferToReauthorize(t *testing.T) {
	key := ForgeKey("primary")
	_, err := stub(nil, gokey.ErrUnavailable).Get(key)

	var failure *KeychainError
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want KeychainError", err)
	}
	if !errors.Is(err, gokey.ErrUnavailable) {
		t.Error("the cause was dropped")
	}
	if strings.Contains(failure.Remedy, "re-authorize") {
		t.Error("the remedy asks the user to re-authorize a store that is not there")
	}
	for _, want := range []string{"no OS credential store", "--allow-plaintext"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
}

func TestEnvStore(t *testing.T) {
	store := NewEnv(func(name string) string {
		if name == "REPOMAP_SECRET_FORGE_PRIMARY" {
			return "from-env"
		}
		return ""
	})
	secret, err := store.Get(ForgeKey("primary"))
	if err != nil || secret.Reveal() != "from-env" {
		t.Fatalf("secret = %v, err = %v", secret, err)
	}
	if _, err := store.Get(ForgeKey("other")); err == nil {
		t.Error("an unset variable produced a secret")
	}
	var readOnly *ReadOnlyError
	if err := store.Set(ForgeKey("primary"), NewSecret("x")); !errors.As(err, &readOnly) {
		t.Errorf("Set error = %v, want ReadOnlyError", err)
	}
	if err := store.Delete(ForgeKey("primary")); !errors.As(err, &readOnly) {
		t.Errorf("Delete error = %v, want ReadOnlyError", err)
	}
}

func TestFileStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	store := NewFile(path)
	if err := store.Set(ForgeKey("primary"), NewSecret(password)); err != nil {
		t.Fatal(err)
	}
	secret, err := store.Get(ForgeKey("primary"))
	if err != nil || secret.Reveal() != password {
		t.Fatalf("secret = %v, err = %v", secret, err)
	}
	// This file holds a database password in plaintext, so the mode is the point
	// of it. Windows cannot report it - Mode().Perm() is synthesised from one
	// read-only attribute and reads 0666 for any writable file - so the check is
	// Unix-only; there the profile directory's ACL is what restricts it. The
	// round trip either side of this runs on every platform.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("permissions = %o, want 600", perm)
		}
	}
	if err := store.Delete(ForgeKey("primary")); err != nil {
		t.Fatal(err)
	}
	var missing *NotFoundError
	if _, err := store.Get(ForgeKey("primary")); !errors.As(err, &missing) {
		t.Errorf("error after delete = %v, want NotFoundError", err)
	}
}

func TestFakeStore(t *testing.T) {
	fake := NewFake()
	if err := fake.Set(LLMKey("main"), NewSecret("key")); err != nil {
		t.Fatal(err)
	}
	if fake.Values[LLMKey("main")] != "key" {
		t.Error("the fake did not record the secret")
	}
	fake.Err = broken
	if _, err := fake.Get(LLMKey("main")); !errors.Is(err, broken) {
		t.Errorf("error = %v, want the injected failure", err)
	}
}

// TestNewKeychainUsesTheBackend catches a build where the gokey calls were not
// wired into the seams, which would otherwise be a nil call at runtime.
func TestNewKeychainUsesTheBackend(t *testing.T) {
	keychain := NewKeychain("")
	if keychain.Service != Service {
		t.Errorf("service = %q, want %q", keychain.Service, Service)
	}
	if keychain.get == nil || keychain.set == nil || keychain.remove == nil ||
		keychain.dropForeign == nil {
		t.Fatal("the backend is not wired")
	}
	if named := NewKeychain("other"); named.Service != "other" {
		t.Errorf("service = %q, want the one given", named.Service)
	}
}
