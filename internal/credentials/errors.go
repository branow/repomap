package credentials

import (
	"errors"
	"fmt"

	"github.com/branow/gokey"
)

// NotFoundError reports a key no store holds.
type NotFoundError struct{ Key string }

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no secret stored for %q", e.Key)
}

// KeychainError reports an OS keychain that could not answer. It is a hard
// error on purpose: repomap never falls back to a plaintext file on its own.
type KeychainError struct {
	Op     string
	Key    string
	Remedy string
	Err    error
}

func (e *KeychainError) Error() string {
	return fmt.Sprintf("keychain %s failed for %q: %v. %s", e.Op, e.Key, e.Err, e.Remedy)
}

func (e *KeychainError) Unwrap() error { return e.Err }

// ReadOnlyError reports a write to a read-only store.
type ReadOnlyError struct{ Store string }

func (e *ReadOnlyError) Error() string {
	return fmt.Sprintf("the %s store is read-only", e.Store)
}

// fail wraps a backend failure with the remedy that fits its cause.
func fail(op, key string, err error) error {
	way := remedy
	switch {
	case errors.Is(err, gokey.ErrBlocked):
		way = blocked(key)
	case errors.Is(err, gokey.ErrUnavailable):
		way = absent
	}
	return &KeychainError{Op: op, Key: key, Err: err, Remedy: way}
}

const remedy = "supply the secret through the " +
	"environment (see repomap help), or opt in to a 0600 file with " +
	"--allow-plaintext or `repomap config set secrets.fallback plaintext`"

// absent: there is no credential store to repair - a headless Linux box with no
// Secret Service, a platform gokey has no backend for - so the only ways out are
// the ones that do not involve one.
const absent = "this machine has no OS credential store, so " + remedy

// blocked: the item's access control does not name this binary - an upgrade
// without an Apple Team ID is a new program to the keychain, as is any rebuild -
// and storing it again from this build repairs that. An approved read on a
// terminal stores it again by itself.
func blocked(key string) string {
	return fmt.Sprintf("re-authorize the item - run repomap on a terminal and "+
		"allow access when the keychain asks, or remove and re-add the entry so this build stores it again - "+
		"or supply the secret through %s", EnvName(key))
}
