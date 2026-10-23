package forge

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/branow/repomap/internal/catalog"
)

// roundTrip serves canned responses. No test in this package opens a socket.
type roundTrip struct {
	responses []*http.Response
	requests  []*http.Request
	err       error
}

func (r *roundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	r.requests = append(r.requests, request)
	if r.err != nil {
		return nil, r.err
	}
	if len(r.responses) == 0 {
		return nil, errors.New("no canned response left")
	}
	response := r.responses[0]
	r.responses = r.responses[1:]
	response.Request = request
	return response, nil
}

func respond(status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     header,
	}
}

func client(transport *roundTrip) *http.Client {
	return &http.Client{Transport: transport}
}

type row struct {
	Name string `json:"name"`
}

func decodeRows(body []byte, _ http.Header) (Page[row], error) {
	var rows []row
	if err := Unmarshal(body, &rows); err != nil {
		return Page[row]{}, err
	}
	return Page[row]{Items: rows}, nil
}

func TestFilterExcludesAndSorts(t *testing.T) {
	cfg := Config{Exclude: []string{"-archive", "sandbox"}}
	got := cfg.Filter([]catalog.Repo{
		{Name: "web"},
		{Name: "api"},
		{Name: "api-archive"},
		{Name: "sandbox-things"},
		{Name: "tools"},
	})

	want := []string{"api", "tools", "web"}
	if len(got) != len(want) {
		t.Fatalf("kept %d repositories, want %d: %v", len(got), len(want), names(got))
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("position %d = %q, want %q", i, got[i].Name, name)
		}
	}
}

// TestAnExactExcludeTakesOnlyThatName: a substring cannot name a repository
// whose name is a prefix of another's without excluding both.
func TestAnExactExcludeTakesOnlyThatName(t *testing.T) {
	repos := []catalog.Repo{{Name: "api"}, {Name: "api-gateway"}, {Name: "legacy-api"}}
	for _, tc := range []struct {
		exclude []string
		want    []string
	}{
		{[]string{"=api"}, []string{"api-gateway", "legacy-api"}},
		{[]string{"api"}, nil},
		{[]string{"=api-gate"}, []string{"api", "api-gateway", "legacy-api"}},
		{[]string{"="}, []string{"api", "api-gateway", "legacy-api"}},
	} {
		got := names(Config{Exclude: tc.exclude}.Filter(repos))
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("exclude %q kept %v, want %v", tc.exclude, got, tc.want)
		}
	}
}

// TestAnEmptyExcludePatternMatchesNothing: strings.Contains is true of every
// string for "", so an empty entry left in a config would silently exclude the
// whole account.
func TestAnEmptyExcludePatternMatchesNothing(t *testing.T) {
	cfg := Config{Exclude: []string{""}}
	if got := cfg.Filter([]catalog.Repo{{Name: "api"}}); len(got) != 1 {
		t.Error("an empty exclude pattern excluded everything")
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		status int
		want   any
	}{
		{status: 200, want: nil},
		{status: 401, want: &AuthError{}},
		{status: 403, want: &AuthError{}},
		{status: 404, want: &NotFoundError{}},
		{status: 429, want: &UnavailableError{}},
		{status: 503, want: &UnavailableError{}},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.status), func(t *testing.T) {
			err := Classify("forge", "acme", "https://example.internal", tt.status)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("status %d produced %v", tt.status, err)
				}
				return
			}
			switch tt.want.(type) {
			case *AuthError:
				var target *AuthError
				if !errors.As(err, &target) {
					t.Errorf("status %d = %v, want an AuthError", tt.status, err)
				}
			case *NotFoundError:
				var target *NotFoundError
				if !errors.As(err, &target) {
					t.Errorf("status %d = %v, want a NotFoundError", tt.status, err)
				}
			case *UnavailableError:
				var target *UnavailableError
				if !errors.As(err, &target) {
					t.Errorf("status %d = %v, want an UnavailableError", tt.status, err)
				}
			}
		})
	}
}

// TestPaginateWalksToExhaustion: a listing that stopped halfway would silently
// index a subset, and a subset is indistinguishable, in the tree it writes,
// from an account that shrank.
func TestPaginateWalksToExhaustion(t *testing.T) {
	transport := &roundTrip{responses: []*http.Response{
		respond(200, `[{"name":"a"}]`, http.Header{"Link": {`<https://example.internal/2>; rel="next"`}}),
		respond(200, `[{"name":"b"}]`, http.Header{"Link": {`<https://example.internal/3>; rel="next"`}}),
		respond(200, `[{"name":"c"}]`, nil),
	}}
	cfg := Config{HTTP: client(transport)}

	rows, err := Paginate(context.Background(), cfg, "forge", "acme",
		"https://example.internal/1", nil,
		func(body []byte, header http.Header) (Page[row], error) {
			page, err := decodeRows(body, header)
			page.Next = nextLink(header.Get("Link"))
			return page, err
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Errorf("walked %d rows, want 3", len(rows))
	}
	if len(transport.requests) != 3 {
		t.Errorf("made %d requests, want 3", len(transport.requests))
	}
}

// TestPaginateStopsALoopingEndpoint: an endpoint answering with a next link
// that points at itself would otherwise run forever.
func TestPaginateStopsALoopingEndpoint(t *testing.T) {
	transport := &roundTrip{}
	for i := 0; i < maxPages+10; i++ {
		transport.responses = append(transport.responses,
			respond(200, `[{"name":"a"}]`, http.Header{"Link": {`<https://example.internal/1>; rel="next"`}}))
	}
	cfg := Config{HTTP: client(transport)}

	rows, err := Paginate(context.Background(), cfg, "forge", "acme",
		"https://example.internal/1", nil,
		func(body []byte, header http.Header) (Page[row], error) {
			page, err := decodeRows(body, header)
			page.Next = nextLink(header.Get("Link"))
			return page, err
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != maxPages {
		t.Errorf("walked %d pages, want the cap of %d", len(rows), maxPages)
	}
}

func TestPaginatePresentsACredential(t *testing.T) {
	tests := []struct {
		name       string
		credential Credential
		want       func(*http.Request) bool
	}{
		{
			name:       "a token with a login name is basic auth",
			credential: Credential{Username: "reader", Token: "secret"},
			want: func(r *http.Request) bool {
				user, pass, ok := r.BasicAuth()
				return ok && user == "reader" && pass == "secret"
			},
		},
		{
			name:       "a token that stands alone is a bearer",
			credential: Credential{Token: "secret"},
			want: func(r *http.Request) bool {
				return r.Header.Get("Authorization") == "Bearer secret"
			},
		},
		{
			name:       "no credential sends no header",
			credential: Credential{},
			want: func(r *http.Request) bool {
				return r.Header.Get("Authorization") == ""
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &roundTrip{responses: []*http.Response{respond(200, `[]`, nil)}}
			cfg := Config{HTTP: client(transport), Credential: tt.credential}
			if _, err := Paginate(context.Background(), cfg, "forge", "acme",
				"https://example.internal/1", nil, decodeRows); err != nil {
				t.Fatal(err)
			}
			if !tt.want(transport.requests[0]) {
				t.Errorf("authorization = %q", transport.requests[0].Header.Get("Authorization"))
			}
		})
	}
}

func TestAnUnreadableAnswerIsNotAnOutage(t *testing.T) {
	transport := &roundTrip{responses: []*http.Response{respond(200, `<html>nope`, nil)}}
	cfg := Config{HTTP: client(transport)}

	_, err := Paginate(context.Background(), cfg, "forge", "acme",
		"https://example.internal/1", nil, decodeRows)
	var response *ResponseError
	if !errors.As(err, &response) {
		t.Fatalf("error = %v, want a ResponseError", err)
	}
}

func TestAnUnregisteredForgeIsRefusedWithTheList(t *testing.T) {
	Register("test-forge", func(Config) (Forge, error) { return nil, nil })
	_, err := New("gitea", Config{})
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %v, want an UnsupportedError", err)
	}
	if len(unsupported.Supported) == 0 {
		t.Error("the refusal does not say what is supported")
	}
}

// nextLink is the Link-header parse the github forge uses, repeated here so
// this package's pagination test does not import a sibling.
func nextLink(link string) string {
	for _, part := range strings.Split(link, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		if start := strings.Index(part, "<"); start >= 0 {
			if end := strings.Index(part[start:], ">"); end > 0 {
				return part[start+1 : start+end]
			}
		}
	}
	return ""
}

func names(repos []catalog.Repo) []string {
	out := make([]string, len(repos))
	for i, r := range repos {
		out[i] = r.Name
	}
	return out
}

// TestCleanRemoteStripsUserinfo: Bitbucket embeds this machine's own username in
// the https clone url, and that url gets written into a committed index.
func TestCleanRemoteStripsUserinfo(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://someone@bitbucket.org/acme/api.git", "https://bitbucket.org/acme/api.git"},
		{"http://someone@git.example.internal/acme/api.git", "http://git.example.internal/acme/api.git"},
		{"https://bitbucket.org/acme/api.git", "https://bitbucket.org/acme/api.git"},
		// ssh's "git@" is the transport's fixed user, not a person.
		{"git@github.com:acme/api.git", "git@github.com:acme/api.git"},
		// An @ after the host belongs to the path, not to userinfo.
		{"https://github.com/acme/weird@name.git", "https://github.com/acme/weird@name.git"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := CleanRemote(tt.in); got != tt.want {
			t.Errorf("CleanRemote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestFilterIsTheGateEveryListingLeavesThrough: a forge that forgot to sanitise
// would otherwise publish a username, so the gate does it for all of them.
func TestFilterIsTheGateEveryListingLeavesThrough(t *testing.T) {
	got := Config{}.Filter([]catalog.Repo{
		{Name: "api", Remote: "https://someone@bitbucket.org/acme/api.git"},
	})
	if got[0].Remote != "https://bitbucket.org/acme/api.git" {
		t.Errorf("remote left the forge as %q", got[0].Remote)
	}
}
