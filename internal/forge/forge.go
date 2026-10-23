// Package forge is where a list of repositories comes from, and the contract
// every source of one keeps. Everything above this package is forge-agnostic.
package forge

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/branow/repomap/internal/catalog"
)

// Forge lists the repositories of one account. It lists and nothing else:
// obtaining a working tree is internal/clone's job, because a repository read
// where it already sits on disk needs no forge at all.
type Forge interface {
	// Name is the forge's identifier, matching the config value that selects it.
	Name() string

	// Repositories is the manifest: every repository the account holds, with
	// whatever the listing already knows. It is the only network call a forge
	// makes, and it must not clone, fetch or read a file.
	Repositories(ctx context.Context) ([]catalog.Repo, error)
}

// Credential is what a forge presents to its API. Both fields empty is an
// unauthenticated forge, which is a supported configuration and not an error.
type Credential struct {
	// Username accompanies a token presented as basic auth. A forge whose token
	// stands alone leaves it empty.
	Username string
	Token    string
}

// Present reports whether there is anything to send.
func (c Credential) Present() bool { return c.Token != "" }

// Config is everything a forge is built from. It carries no config.Workspace:
// this package does not read the user's file, it is handed the values.
type Config struct {
	// Slug is the account repositories are listed under.
	Slug string
	// BaseURL overrides the public endpoint for a self-hosted forge.
	BaseURL string
	// Dir is the directory of checkouts the local forge reads.
	Dir string
	// Exclude drops repositories whose name contains any of these substrings, or,
	// for an entry starting with "=", whose name is exactly the rest of it.
	Exclude []string
	// Deprecated names repositories, exactly, that stay listed but are marked
	// dead: never cloned, never described.
	Deprecated []string
	Credential
	// HTTP is the client every request goes through, so a test never opens a
	// socket. Nil means http.DefaultClient.
	HTTP *http.Client
}

// Client is the http client a forge sends through.
func (c Config) Client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Filter is the gate every forge's listing leaves through. It drops excluded
// repositories, marks deprecated ones, strips any credential a clone url
// carries, and sorts the rest by name so the manifest does not depend on the
// order a paginated API answered in.
func (c Config) Filter(repos []catalog.Repo) []catalog.Repo {
	deprecated := make(map[string]bool, len(c.Deprecated))
	for _, name := range c.Deprecated {
		deprecated[name] = true
	}
	kept := make([]catalog.Repo, 0, len(repos))
	for _, repo := range repos {
		if c.excluded(repo.Name) {
			continue
		}
		repo.Deprecated = deprecated[repo.Name]
		repo.Remote = CleanRemote(repo.Remote)
		kept = append(kept, repo)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Name < kept[j].Name })
	return kept
}

// CleanRemote strips userinfo from an http(s) clone url.
//
// Bitbucket hands back "https://someone@bitbucket.org/acme/api.git". That
// username is this machine's account, and the url is written into an index that
// gets committed and shared - so it would leak one person's identity into
// everybody's checkout, and show up in the argument list of every clone this
// tool runs. An ssh url's "git@" is the transport's own fixed user, not somebody
// in particular, and is left alone.
func CleanRemote(remote string) string {
	for _, scheme := range []string{"https://", "http://"} {
		if !strings.HasPrefix(remote, scheme) {
			continue
		}
		rest := remote[len(scheme):]
		at := strings.Index(rest, "@")
		slash := strings.Index(rest, "/")
		if at > 0 && (slash == -1 || at < slash) {
			return scheme + rest[at+1:]
		}
	}
	return remote
}

// ExactPrefix marks an exclude entry that matches a whole name. A substring
// cannot name "api" without also taking "api-gateway" with it.
const ExactPrefix = "="

// excludeForms is how an exclude entry is read, asked in order: the first form
// whose prefix the entry carries decides, and the bare form is the fallback.
var excludeForms = []struct {
	prefix string
	match  func(name, pattern string) bool
}{
	{ExactPrefix, func(name, pattern string) bool { return name == pattern }},
	{"", strings.Contains},
}

func (c Config) excluded(name string) bool {
	for _, entry := range c.Exclude {
		for _, form := range excludeForms {
			if !strings.HasPrefix(entry, form.prefix) {
				continue
			}
			// An empty pattern would match every name as a substring and
			// silently exclude the whole account.
			pattern := entry[len(form.prefix):]
			if pattern != "" && form.match(name, pattern) {
				return true
			}
			break
		}
	}
	return false
}

// Factory builds one forge from its configuration.
type Factory func(Config) (Forge, error)

var registry = map[string]Factory{}

// Register adds a forge implementation. Each implementation calls this from its
// own package init, so adding a forge is adding a package and importing it -
// never editing a switch here.
func Register(name string, factory Factory) { registry[name] = factory }

// New builds the named forge.
func New(name string, cfg Config) (Forge, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, &UnsupportedError{Forge: name, Supported: Names()}
	}
	return factory(cfg)
}

// Names lists the registered forges in stable order.
func Names() []string {
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
