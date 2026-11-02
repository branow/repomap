package credentials

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/branow/gokey"
)

// fakeKeychain models the macOS rules the Keychain has to work around, with no
// keychain present. An item marked foreign belongs to another program - an
// earlier build, or /usr/bin/security - and is released only through the
// dialog; storing over it either rebinds it or, when ownerEdit is set, is
// refused the way the keychain refuses to replace an item /usr/bin/security
// owns, so that only dropForeign removes it.
type fakeKeychain struct {
	items   map[string]string
	foreign map[string]bool

	ownerEdit bool  // set refuses to replace a foreign item
	setErr    error // when non-nil, every set fails with it

	dropped []string // accounts deleted through dropForeign
}

func newFakeKeychain() *fakeKeychain {
	return &fakeKeychain{items: map[string]string{}, foreign: map[string]bool{}}
}

var errOwnerEdit = errors.New("the keychain refused the request (OSStatus -25244)")

func (f *fakeKeychain) get(_, account string, prompt bool) (string, error) {
	value, ok := f.items[account]
	switch {
	case !ok:
		return "", gokey.ErrNotFound
	case f.foreign[account] && !prompt:
		return "", gokey.ErrBlocked
	}
	return value, nil
}

func (f *fakeKeychain) set(_, account, secret string, _ bool) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.foreign[account] && f.ownerEdit {
		return errOwnerEdit
	}
	f.items[account] = secret
	delete(f.foreign, account)
	return nil
}

func (f *fakeKeychain) remove(_, account string, _ bool) error {
	if _, ok := f.items[account]; !ok {
		return gokey.ErrNotFound
	}
	delete(f.items, account)
	delete(f.foreign, account)
	return nil
}

func (f *fakeKeychain) drop(_, account string) error {
	f.dropped = append(f.dropped, account)
	return f.remove("", account, false)
}

// keychainOver returns a Keychain whose every seam is the fake, and the buffer
// its warnings go to.
func keychainOver(f *fakeKeychain, prompt bool) (*Keychain, *bytes.Buffer) {
	warn := &bytes.Buffer{}
	k := newKeychain("test", prompt, warn)
	k.get, k.set, k.remove, k.dropForeign = f.get, f.set, f.remove, f.drop
	return k, warn
}

// TestApprovedReadStoresTheItemAgain is the upgrade path: an item an earlier
// build stored is refused silently, released through the dialog, and stored
// again so the next read is silent.
func TestApprovedReadStoresTheItemAgain(t *testing.T) {
	key := ForgeKey("primary")
	fake := newFakeKeychain()
	fake.items[key], fake.foreign[key] = password, true
	keychain, warn := keychainOver(fake, true)
	seen := spy(keychain)

	secret, err := keychain.Get(key)
	if err != nil || secret.Reveal() != password {
		t.Fatalf("Get = %v, %v; want the stored value", secret, err)
	}
	want := []call{{"get", false}, {"get", true}, {"set", true}}
	if len(*seen) != len(want) {
		t.Fatalf("calls = %v, want %v", *seen, want)
	}
	for i, got := range *seen {
		if got != want[i] {
			t.Errorf("call %d = %v, want %v", i, got, want[i])
		}
	}
	if fake.foreign[key] || fake.items[key] != password {
		t.Error("the item was not stored again under this build")
	}
	if warn.Len() != 0 {
		t.Errorf("warned %q on a re-save that worked", warn.String())
	}

	// The second read needs no dialog at all.
	*seen = nil
	if _, err := keychain.Get(key); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if len(*seen) != 1 || (*seen)[0] != (call{"get", false}) {
		t.Errorf("second read calls = %v, want one silent get", *seen)
	}
}

// TestBlockedReadWithoutAWatcherLeavesTheItem: nobody can answer the dialog, so
// the read fails with its remedy, is never retried with a prompt, and the item
// is neither rewritten nor removed.
func TestBlockedReadWithoutAWatcherLeavesTheItem(t *testing.T) {
	key := ForgeKey("primary")
	fake := newFakeKeychain()
	fake.items[key], fake.foreign[key] = password, true
	keychain, warn := keychainOver(fake, false)
	seen := spy(keychain)

	_, err := keychain.Get(key)
	if !errors.Is(err, gokey.ErrBlocked) {
		t.Fatalf("error = %v, want ErrBlocked", err)
	}
	if len(*seen) != 1 || (*seen)[0] != (call{"get", false}) {
		t.Errorf("calls = %v, want one silent get", *seen)
	}
	if !fake.foreign[key] || fake.items[key] != password {
		t.Error("a refused read changed the item")
	}
	if len(fake.dropped) != 0 || warn.Len() != 0 {
		t.Errorf("dropped %v, warned %q; want neither", fake.dropped, warn.String())
	}
}

// TestFailedReSaveWarnsAndStillReads: the read was approved and has the value,
// so a re-save the keychain refuses costs only the dialog next time - a warning,
// not a failed command. The warning names the key and never the secret.
func TestFailedReSaveWarnsAndStillReads(t *testing.T) {
	key := ForgeKey("primary")
	fake := newFakeKeychain()
	fake.items[key], fake.foreign[key] = password, true
	fake.setErr = errors.New("the keychain refused the request (OSStatus -61)")
	keychain, warn := keychainOver(fake, true)

	secret, err := keychain.Get(key)
	if err != nil || secret.Reveal() != password {
		t.Fatalf("Get = %v, %v; want the stored value despite the failed re-save", secret, err)
	}
	if !strings.Contains(warn.String(), "warning:") || !strings.Contains(warn.String(), key) {
		t.Errorf("warning = %q, want one naming %q", warn.String(), key)
	}
	if strings.Contains(warn.String(), password) {
		t.Error("the warning carries the secret")
	}
	if len(fake.dropped) != 0 {
		t.Errorf("dropped %v after a failure other than -25244", fake.dropped)
	}
}

// TestSetReplacesAnItemAnotherProgramOwns: the keychain refuses to replace an
// item /usr/bin/security owns, its owner deletes it, and the add is tried once
// more, now bound to this binary.
func TestSetReplacesAnItemAnotherProgramOwns(t *testing.T) {
	key := ForgeKey("primary")
	fake := newFakeKeychain()
	fake.items[key], fake.foreign[key] = "old", true
	fake.ownerEdit = true
	keychain, _ := keychainOver(fake, false)

	if err := keychain.Set(key, NewSecret(password)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if len(fake.dropped) != 1 || fake.dropped[0] != key {
		t.Errorf("dropped = %v, want exactly %q", fake.dropped, key)
	}
	if fake.items[key] != password || fake.foreign[key] {
		t.Error("the item was not stored again under this build")
	}
}

// TestSetKeepsTheItemOnOtherFailures: only -25244 may lead to a delete. After
// any other refusal the retry would most likely fail too, and the item would be
// gone.
func TestSetKeepsTheItemOnOtherFailures(t *testing.T) {
	for _, failure := range []error{
		gokey.ErrBlocked,
		gokey.ErrUnavailable,
		errors.New("the keychain refused the request (OSStatus -25299)"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			key := ForgeKey("primary")
			fake := newFakeKeychain()
			fake.items[key] = "old"
			fake.setErr = failure
			keychain, _ := keychainOver(fake, false)

			err := keychain.Set(key, NewSecret(password))
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want %v", err, failure)
			}
			if len(fake.dropped) != 0 {
				t.Errorf("dropped %v after %v", fake.dropped, failure)
			}
			if fake.items[key] != "old" {
				t.Error("the existing item was lost")
			}
		})
	}
}

func TestIsOwnerEdit(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{err: nil, want: false},
		{err: errOwnerEdit, want: true},
		{err: errors.New("gokey: write \"repomap\"/\"forge:x\": the keychain refused the request (OSStatus -25244)"), want: true},
		{err: errors.New("the keychain refused the request (OSStatus -25243)"), want: false},
		{err: gokey.ErrBlocked, want: false},
	}
	for _, tt := range tests {
		if got := isOwnerEdit(tt.err); got != tt.want {
			t.Errorf("isOwnerEdit(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
