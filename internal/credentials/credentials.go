// Package credentials is the only place in repomap that touches a secret.
package credentials

import (
	"encoding/json"
	"strings"
)

const (
	forgePrefix = "forge:"
	llmPrefix   = "llm:"
)

// Service is the keychain service every repomap entry is filed under.
const Service = "repomap"

// ForgeKey is the store key holding a forge's access token.
func ForgeKey(name string) string { return forgePrefix + name }

// LLMKey is the store key holding a backend's api key.
func LLMKey(name string) string { return llmPrefix + name }

// Secret is an opaque credential: printing, formatting and marshalling all
// yield a placeholder. Reveal is the single way out.
type Secret struct{ value string }

// NewSecret wraps a raw credential value.
func NewSecret(v string) Secret { return Secret{value: v} }

// Reveal returns the raw value.
func (s Secret) Reveal() string { return s.value }

func (s Secret) Empty() bool { return s.value == "" }

func (s Secret) String() string { return redacted }

func (s Secret) GoString() string { return redacted }

func (s Secret) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

func (s Secret) MarshalYAML() (any, error) { return redacted, nil }

const redacted = "[redacted]"

// Store holds secrets by key. Deleting an absent key is not an error.
type Store interface {
	Get(key string) (Secret, error)
	Set(key string, secret Secret) error
	Delete(key string) error
}

// EnvName maps a store key to the variable that can supply it: forge:main becomes
// REPOMAP_SECRET_FORGE_MAIN.
func EnvName(key string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, key)
	return "REPOMAP_SECRET_" + strings.ToUpper(clean)
}
