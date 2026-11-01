package credentials

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/branow/gokey"
)

// Keychain stores secrets in the OS credential store, addressed by service plus
// account, the account being our own key. gokey carries the per-platform
// backends; what this type adds is repomap's error vocabulary around them, and
// the repair of an item an earlier build stored.
//
// The operations are fields so a unit test can drive every branch with no
// credential store present.
type Keychain struct {
	Service string

	// prompt says a call may put the OS authorization dialog on screen. The
	// zero value refuses: a dialog nobody can see blocks forever.
	prompt bool

	// warn receives what is worth saying but not worth failing over: a read
	// that succeeded but could not store the item again. Nil discards it.
	warn io.Writer

	get    func(service, account string, prompt bool) (string, error)
	set    func(service, account, secret string, prompt bool) error
	remove func(service, account string, prompt bool) error

	// dropForeign deletes an item another program owns; see Set.
	dropForeign func(service, account string) error
}

// NewKeychain returns an OS-keychain store that never lets the keychain prompt.
// Prompting is opt-in through Options: only the caller knows whether a human is
// watching a terminal.
func NewKeychain(service string) *Keychain { return newKeychain(service, false, nil) }

func newKeychain(service string, prompt bool, warn io.Writer) *Keychain {
	if service == "" {
		service = Service
	}
	return &Keychain{
		Service:     service,
		prompt:      prompt,
		warn:        warn,
		get:         itemGet,
		set:         itemSet,
		remove:      itemDelete,
		dropForeign: deleteWithSecurityTool,
	}
}

func itemGet(service, account string, prompt bool) (string, error) {
	return gokey.Get(service, account, gokey.WithPrompt(prompt))
}

func itemSet(service, account, secret string, prompt bool) error {
	return gokey.Set(service, account, secret, gokey.WithPrompt(prompt))
}

func itemDelete(service, account string, prompt bool) error {
	return gokey.Delete(service, account, gokey.WithPrompt(prompt))
}

// Get reads silently first: an item this binary stored needs no dialog, and
// only a refusal tells that it does not. Only then, and only when a human is
// watching, is the read repeated with the dialog allowed.
func (k *Keychain) Get(key string) (Secret, error) {
	value, err := k.get(k.Service, key, false)
	approved := false
	if errors.Is(err, gokey.ErrBlocked) && k.prompt {
		value, err = k.get(k.Service, key, true)
		approved = err == nil
	}
	switch {
	case errors.Is(err, gokey.ErrNotFound):
		return Secret{}, &NotFoundError{Key: key}
	case err != nil:
		return Secret{}, fail("read", key, err)
	}
	if approved {
		k.rebind(key, value)
	}
	return NewSecret(value), nil
}

func (k *Keychain) Set(key string, secret Secret) error {
	if err := k.store(key, secret.Reveal()); err != nil {
		return fail("write", key, err)
	}
	return nil
}

func (k *Keychain) Delete(key string) error {
	err := k.remove(k.Service, key, k.prompt)
	switch {
	case err == nil, errors.Is(err, gokey.ErrNotFound):
		return nil
	default:
		return fail("delete", key, err)
	}
}

// store writes the item. When the keychain refuses to replace an item another
// program owns, that owner - /usr/bin/security, for anything stored through it -
// deletes it without asking, and the write is tried once more.
func (k *Keychain) store(key, value string) error {
	err := k.set(k.Service, key, value, k.prompt)
	if isOwnerEdit(err) && k.dropForeign(k.Service, key) == nil {
		err = k.set(k.Service, key, value, k.prompt)
	}
	return err
}

// rebind stores an item that needed the dialog again, so it is bound to this
// binary and the next read raises none. Release builds carry no Apple Team ID,
// so the keychain partitions an item by cdhash and every upgrade is a new
// program to it. A failure costs only that dialog on later reads, so it warns
// rather than failing a read that already succeeded.
func (k *Keychain) rebind(key, value string) {
	if err := k.store(key, value); err != nil && k.warn != nil {
		fmt.Fprintf(k.warn, "warning: could not store %q again under this build, "+
			"so the keychain will keep asking for it: %v\n", key, err)
	}
}

// isOwnerEdit reports the keychain refusing to replace an item another program
// owns (errSecInvalidOwnerEdit). gokey has no sentinel for it, so the status it
// keeps in the message is matched. Only this refusal may lead to deleting the
// old item: after any other failure the retry would most likely fail too, and
// the item would be gone.
func isOwnerEdit(err error) bool {
	return err != nil && strings.Contains(err.Error(), "OSStatus -25244")
}

// deleteWithSecurityTool deletes an item through /usr/bin/security. It only
// deletes: the item's address is passed, and no secret is read or written
// through it. The status above is macOS-only, so no other platform reaches it.
func deleteWithSecurityTool(service, account string) error {
	out, err := exec.Command("/usr/bin/security", "delete-generic-password",
		"-s", service, "-a", account).CombinedOutput()
	if err != nil {
		return fmt.Errorf("security delete-generic-password: %w: %s", err,
			strings.TrimSpace(string(out)))
	}
	return nil
}
