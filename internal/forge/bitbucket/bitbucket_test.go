package bitbucket

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/branow/repomap/internal/forge"
)

type roundTrip struct {
	bodies   []string
	requests []*http.Request
}

func (r *roundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	r.requests = append(r.requests, request)
	body := `{"values":[]}`
	if len(r.bodies) > 0 {
		body, r.bodies = r.bodies[0], r.bodies[1:]
	}
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}, nil
}

func build(t *testing.T, transport *roundTrip) forge.Forge {
	t.Helper()
	f, err := New(forge.Config{Slug: "acme", HTTP: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

const listing = `{
  "values": [
    {"slug":"api","description":"the order api","updated_on":"2026-09-01T10:00:00.000000+00:00",
     "mainbranch":{"name":"main"},
     "links":{"clone":[
       {"name":"https","href":"https://user@bitbucket.org/acme/api.git"},
       {"name":"ssh","href":"git@bitbucket.org:acme/api.git"}]}},
    {"slug":"web","description":"","updated_on":"2026-08-01T10:00:00.000000+00:00",
     "mainbranch":{"name":"trunk"},
     "links":{"clone":[{"name":"https","href":"https://user@bitbucket.org/acme/web.git"}]}}
  ]
}`

func TestRepositories(t *testing.T) {
	transport := &roundTrip{bodies: []string{listing}}
	got, err := build(t, transport).Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("listed %d repositories, want 2", len(got))
	}
	if got[0].Name != "api" || got[0].Summary != "the order api" || got[0].Branch != "main" {
		t.Errorf("api = %+v", got[0])
	}
	if got[0].Activity.String() != "2026-09-01T10:00:00.000000+00:00" {
		t.Errorf("activity = %q, want updated_on verbatim", got[0].Activity)
	}
}

// TestSSHIsPreferredOverHTTPS: the https link Bitbucket returns carries no
// credential and would prompt, and a build that prompts hangs forever.
func TestSSHIsPreferredOverHTTPS(t *testing.T) {
	got, err := build(t, &roundTrip{bodies: []string{listing}}).
		Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Remote != "git@bitbucket.org:acme/api.git" {
		t.Errorf("remote = %q, want the ssh url", got[0].Remote)
	}
	// With no ssh link, https is better than nothing - and Bitbucket's https url
	// carries this machine's username, which must not reach a committed index.
	if got[1].Remote != "https://bitbucket.org/acme/web.git" {
		t.Errorf("remote = %q, want the https url with no userinfo", got[1].Remote)
	}
}

// TestTheListingAsksForOnlyTheColumnsItUses: Bitbucket returns a large object
// per repository by default and honours a field selector, which is the
// difference between a few hundred kilobytes and a few megabytes on a large
// workspace.
func TestTheListingAsksForOnlyTheColumnsItUses(t *testing.T) {
	transport := &roundTrip{bodies: []string{listing}}
	if _, err := build(t, transport).Repositories(context.Background()); err != nil {
		t.Fatal(err)
	}
	query := transport.requests[0].URL.Query()
	if query.Get("fields") == "" {
		t.Error("the listing asks for every field Bitbucket has")
	}
	if query.Get("pagelen") != "100" {
		t.Errorf("pagelen = %q, want the largest page Bitbucket serves", query.Get("pagelen"))
	}
	if query.Get("sort") != "name" {
		t.Errorf("sort = %q, want a stable order", query.Get("sort"))
	}
}

// TestPaginationFollowsTheEnvelope: Bitbucket puts the next page in the body,
// where GitHub puts it in a header.
func TestPaginationFollowsTheEnvelope(t *testing.T) {
	first := `{"next":"https://api.bitbucket.org/2.0/repositories/acme?page=2",
	           "values":[{"slug":"api"}]}`
	second := `{"values":[{"slug":"web"}]}`
	transport := &roundTrip{bodies: []string{first, second}}

	got, err := build(t, transport).Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("walked %d repositories, want 2", len(got))
	}
	if len(transport.requests) != 2 {
		t.Errorf("made %d requests, want 2", len(transport.requests))
	}
}

func TestPingReadsOnePage(t *testing.T) {
	transport := &roundTrip{bodies: []string{listing}}
	err := Ping(context.Background(), forge.Config{
		Slug: "acme",
		HTTP: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 1 {
		t.Errorf("a probe made %d requests, want 1", len(transport.requests))
	}
}
