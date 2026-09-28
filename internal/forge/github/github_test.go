package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/branow/repomap/internal/forge"
)

type roundTrip struct {
	byPath    map[string]*http.Response
	requested []string
}

func (r *roundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	r.requested = append(r.requested, request.URL.Path)
	response, ok := r.byPath[request.URL.Path]
	if !ok {
		return respond(404, `{"message":"Not Found"}`), nil
	}
	return response, nil
}

func respond(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}
}

func build(t *testing.T, transport *roundTrip, slug string) forge.Forge {
	t.Helper()
	f, err := New(forge.Config{Slug: slug, HTTP: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

const repos = `[
  {"name":"api","description":"the order api","default_branch":"main",
   "ssh_url":"git@github.com:acme/api.git","clone_url":"https://github.com/acme/api.git",
   "pushed_at":"2026-09-01T10:00:00Z"},
  {"name":"old","description":"","default_branch":"master",
   "ssh_url":"git@github.com:acme/old.git","archived":true,
   "pushed_at":"2019-01-01T00:00:00Z"},
  {"name":"dead","disabled":true,"ssh_url":"git@github.com:acme/dead.git"}
]`

func TestRepositories(t *testing.T) {
	transport := &roundTrip{byPath: map[string]*http.Response{
		"/orgs/acme/repos": respond(200, repos),
	}}
	got, err := build(t, transport, "acme").Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("listed %d repositories, want 2 (a disabled one is unreadable)", len(got))
	}
	api := got[0]
	if api.Name != "api" || api.Summary != "the order api" || api.Branch != "main" {
		t.Errorf("api = %+v", api)
	}
	// ssh, because that is the transport a developer's git is already
	// authenticated for.
	if api.Remote != "git@github.com:acme/api.git" {
		t.Errorf("remote = %q, want the ssh url", api.Remote)
	}
	if !got[1].Archived {
		t.Error("an archived repository was not marked as one")
	}
}

// TestTheSignalIsPushedAtNotUpdatedAt: updated_at moves when somebody stars the
// repository or edits its description, neither of which changes a file, and a
// signal that over-reports costs a clone per run.
func TestTheSignalIsPushedAtNotUpdatedAt(t *testing.T) {
	transport := &roundTrip{byPath: map[string]*http.Response{
		"/orgs/acme/repos": respond(200,
			`[{"name":"api","pushed_at":"2026-09-01T10:00:00Z","updated_at":"2026-09-27T00:00:00Z"}]`),
	}}
	got, err := build(t, transport, "acme").Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Activity.String() != "2026-09-01T10:00:00Z" {
		t.Errorf("activity = %q, want pushed_at", got[0].Activity)
	}
}

// TestAUserFallsBackFromTheOrgEndpoint: the caller does not know whether a slug
// is an organisation or a user, and this is the only place the forge guesses.
func TestAUserFallsBackFromTheOrgEndpoint(t *testing.T) {
	transport := &roundTrip{byPath: map[string]*http.Response{
		"/users/someone/repos": respond(200, `[{"name":"dotfiles"}]`),
	}}
	got, err := build(t, transport, "someone").Repositories(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "dotfiles" {
		t.Fatalf("listed %+v", got)
	}
	if len(transport.requested) != 2 || transport.requested[0] != "/orgs/someone/repos" {
		t.Errorf("request order = %v, want the org endpoint tried first", transport.requested)
	}
}

func TestNext(t *testing.T) {
	tests := []struct {
		link string
		want string
	}{
		{link: "", want: ""},
		{link: `<https://api.github.com/x?page=2>; rel="next"`, want: "https://api.github.com/x?page=2"},
		{
			link: `<https://api.github.com/x?page=1>; rel="prev", ` +
				`<https://api.github.com/x?page=3>; rel="next", ` +
				`<https://api.github.com/x?page=9>; rel="last"`,
			want: "https://api.github.com/x?page=3",
		},
		{link: `<https://api.github.com/x?page=9>; rel="last"`, want: ""},
	}
	for _, tt := range tests {
		if got := Next(tt.link); got != tt.want {
			t.Errorf("Next(%q) = %q, want %q", tt.link, got, tt.want)
		}
	}
}
