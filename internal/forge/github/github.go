// Package github lists the repositories of one GitHub organisation or user.
package github

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/forge"
)

// Name is the value that selects this forge in a workspace entry.
const Name = "github"

// DefaultBaseURL is github.com's API root. A GitHub Enterprise Server install
// is the same API under a different root, which is what BaseURL is for.
const DefaultBaseURL = "https://api.github.com"

// pageSize is the largest GitHub serves.
const pageSize = 100

func init() { forge.Register(Name, New) }

type github struct{ cfg forge.Config }

// New builds a GitHub forge.
func New(cfg forge.Config) (forge.Forge, error) { return &github{cfg: cfg}, nil }

func (g *github) Name() string { return Name }

type repository struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	SSHURL        string `json:"ssh_url"`
	CloneURL      string `json:"clone_url"`
	Archived      bool   `json:"archived"`
	Disabled      bool   `json:"disabled"`
	PushedAt      string `json:"pushed_at"`
}

func (g *github) Repositories(ctx context.Context) ([]catalog.Repo, error) {
	base := strings.TrimSuffix(g.cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	query := url.Values{}
	query.Set("per_page", strconv.Itoa(pageSize))
	query.Set("sort", "full_name")
	// An organisation and a user are different endpoints on GitHub and the
	// caller does not know which the slug is, so the org endpoint is tried and
	// a 404 falls through to the user one. That is the only place this forge
	// guesses, and it guesses once per run.
	orgURL := base + "/orgs/" + url.PathEscape(g.cfg.Slug) + "/repos?" + query.Encode()
	rows, err := g.list(ctx, orgURL)
	var missing *forge.NotFoundError
	if err != nil && asNotFound(err, &missing) {
		userURL := base + "/users/" + url.PathEscape(g.cfg.Slug) + "/repos?" + query.Encode()
		rows, err = g.list(ctx, userURL)
	}
	if err != nil {
		return nil, err
	}

	repos := make([]catalog.Repo, 0, len(rows))
	for _, row := range rows {
		if row.Disabled {
			continue
		}
		repos = append(repos, catalog.Repo{
			Name:    row.Name,
			Summary: strings.TrimSpace(row.Description),
			Branch:  row.DefaultBranch,
			Remote:  remote(row),
			// pushed_at, not updated_at: updated_at moves when somebody stars
			// the repository or edits its description, neither of which changes
			// a file. A signal that over-reports costs a clone per run.
			Activity: catalog.Signal(row.PushedAt),
			Archived: row.Archived,
		})
	}
	return g.cfg.Filter(repos), nil
}

func (g *github) list(ctx context.Context, first string) ([]repository, error) {
	return forge.Paginate(ctx, g.cfg, Name, g.cfg.Slug, first,
		func(r *http.Request) { r.Header.Set("Accept", "application/vnd.github+json") },
		func(body []byte, header http.Header) (forge.Page[repository], error) {
			var rows []repository
			if err := forge.Unmarshal(body, &rows); err != nil {
				return forge.Page[repository]{}, err
			}
			return forge.Page[repository]{Items: rows, Next: Next(header.Get("Link"))}, nil
		})
}

func remote(row repository) string {
	if row.SSHURL != "" {
		return row.SSHURL
	}
	return row.CloneURL
}

func asNotFound(err error, target **forge.NotFoundError) bool {
	notFound, ok := err.(*forge.NotFoundError)
	if ok {
		*target = notFound
	}
	return ok
}

// linkNext reads a next page out of a Link header. GitHub paginates by header
// rather than in the body, which is why this forge decodes a bare array while
// Bitbucket decodes an envelope.
var linkNext = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// Next extracts the next-page URL from a Link header value.
func Next(link string) string {
	if match := linkNext.FindStringSubmatch(link); match != nil {
		return match[1]
	}
	return ""
}
