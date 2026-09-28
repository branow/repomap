// Package bitbucket lists the repositories of one Bitbucket Cloud workspace.
package bitbucket

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/forge"
)

// Name is the value that selects this forge in a workspace entry.
const Name = "bitbucket"

// DefaultBaseURL is Bitbucket Cloud's API root.
const DefaultBaseURL = "https://api.bitbucket.org/2.0"

// pageSize is the largest Bitbucket serves; a smaller one only buys more round
// trips for the same rows.
const pageSize = 100

// fields asks Bitbucket for exactly the columns the manifest needs. The API
// returns a large object per repository by default and honours a field
// selector, so this is the difference between a few hundred kilobytes and a few
// megabytes on a large workspace.
const fields = "next,values.slug,values.description,values.updated_on," +
	"values.is_private,values.mainbranch.name,values.links.clone,values.parent"

func init() { forge.Register(Name, New) }

type bitbucket struct{ cfg forge.Config }

// New builds a Bitbucket forge.
func New(cfg forge.Config) (forge.Forge, error) { return &bitbucket{cfg: cfg}, nil }

func (b *bitbucket) Name() string { return Name }

// repository is the shape this forge reads back, named for what Bitbucket
// sends rather than for what the catalog calls it.
type repository struct {
	Slug        string `json:"slug"`
	Description string `json:"description"`
	UpdatedOn   string `json:"updated_on"`
	MainBranch  struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
	Links struct {
		Clone []struct {
			Name string `json:"name"`
			HREF string `json:"href"`
		} `json:"clone"`
	} `json:"links"`
}

type page struct {
	Next   string       `json:"next"`
	Values []repository `json:"values"`
}

func (b *bitbucket) Repositories(ctx context.Context) ([]catalog.Repo, error) {
	base := b.cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	query := url.Values{}
	query.Set("pagelen", strconv.Itoa(pageSize))
	query.Set("fields", fields)
	// Sorted by name so a run interrupted mid-listing resumes over the same
	// order rather than over whatever was most recently pushed to.
	query.Set("sort", "name")
	first := strings.TrimSuffix(base, "/") + "/repositories/" +
		url.PathEscape(b.cfg.Slug) + "?" + query.Encode()

	rows, err := forge.Paginate(ctx, b.cfg, Name, b.cfg.Slug, first, nil,
		func(body []byte, _ http.Header) (forge.Page[repository], error) {
			var p page
			if err := forge.Unmarshal(body, &p); err != nil {
				return forge.Page[repository]{}, err
			}
			return forge.Page[repository]{Items: p.Values, Next: p.Next}, nil
		})
	if err != nil {
		return nil, err
	}

	repos := make([]catalog.Repo, 0, len(rows))
	for _, row := range rows {
		repos = append(repos, catalog.Repo{
			Name:    row.Slug,
			Summary: strings.TrimSpace(row.Description),
			Branch:  row.MainBranch.Name,
			Remote:  cloneURL(row),
			// Bitbucket's updated_on moves on a push and on little else, which
			// is what makes it usable as the free tier of the staleness check.
			Activity: catalog.Signal(row.UpdatedOn),
		})
	}
	return b.cfg.Filter(repos), nil
}

// cloneURL prefers ssh, because that is the transport a developer's git is
// already authenticated for; the https link Bitbucket returns carries no
// credential and would prompt.
func cloneURL(row repository) string {
	var https string
	for _, link := range row.Links.Clone {
		switch link.Name {
		case "ssh":
			return link.HREF
		case "https":
			https = link.HREF
		}
	}
	return https
}

// Ping is not part of the Forge interface: the manifest call is the probe, and
// a separate reachability endpoint would be a second thing to keep correct. It
// exists for doctor, which wants a verdict without pulling a whole listing.
func Ping(ctx context.Context, cfg forge.Config) error {
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	target := strings.TrimSuffix(base, "/") + "/repositories/" +
		url.PathEscape(cfg.Slug) + "?pagelen=1&fields=values.slug"
	_, err := forge.Paginate(ctx, cfg, Name, cfg.Slug, target, nil,
		func(body []byte, _ http.Header) (forge.Page[repository], error) {
			var p page
			if err := forge.Unmarshal(body, &p); err != nil {
				return forge.Page[repository]{}, err
			}
			// No Next: one page is the whole probe.
			return forge.Page[repository]{Items: p.Values}, nil
		})
	return err
}
