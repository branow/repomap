package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/cmdutil"
	"github.com/branow/repomap/internal/config"
)

// fakeSource answers a listing with no forge behind it.
type fakeSource struct {
	repos []catalog.Repo
	err   error
}

func (f *fakeSource) Forge() string { return "fake" }
func (f *fakeSource) Slug() string  { return "acme" }
func (f *fakeSource) Repositories(context.Context) ([]catalog.Repo, error) {
	return f.repos, f.err
}
func (f *fakeSource) Reachable(context.Context, catalog.Repo) error { return nil }
func (f *fakeSource) Scan(context.Context, catalog.Repo) (catalog.Tree, error) {
	return catalog.Tree{}, nil
}
func (f *fakeSource) Close() error { return nil }

// TestAnEmptyRemoteListingIsAFailure pins what a live Bitbucket run exposed:
// a private workspace you cannot see answers HTTP 200 with an empty list rather
// than refusing the request. Every check passed, doctor exited 0, and the next
// step was a successful build of an index of nothing.
func TestAnEmptyRemoteListingIsAFailure(t *testing.T) {
	tests := []struct {
		name  string
		entry config.Workspace
		want  string
	}{
		{
			name:  "an unauthenticated private account",
			entry: config.Workspace{Forge: config.Bitbucket, Slug: "acme", Auth: config.None},
			want:  "add a token",
		},
		{
			name:  "a token that can see nothing, or a typo in the slug",
			entry: config.Workspace{Forge: config.GitHub, Slug: "acme", Auth: config.Token},
			want:  "check the account name",
		},
		{
			name:  "a local directory with no checkouts in it",
			entry: config.Workspace{Forge: config.Local, Dir: "/empty", Auth: config.None},
			want:  "holds no git checkouts",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := emptyListing(tt.entry)
			if err == nil {
				t.Fatal("an empty listing was reported as healthy")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("message = %q, want it to mention %q", err, tt.want)
			}
			// It is the user's configuration to fix, not an outage to wait out.
			if got := cmdutil.ExitCode(err); got != cmdutil.ExitValidation {
				t.Errorf("exit code = %d, want %d", got, cmdutil.ExitValidation)
			}
		})
	}
}

// TestDoctorStopsAtTheEmptyListing: the checks after it would otherwise report
// green on a workspace that cannot work.
func TestDoctorStopsAtTheEmptyListing(t *testing.T) {
	p := &preflight{
		entry:  config.Workspace{Forge: config.Bitbucket, Slug: "acme", Auth: config.None},
		source: &fakeSource{repos: nil},
	}
	if _, err := listing(context.Background(), p); err == nil {
		t.Fatal("an empty remote listing passed the listing check")
	}
}

func TestANonEmptyListingPasses(t *testing.T) {
	p := &preflight{
		entry:  config.Workspace{Forge: config.GitHub, Slug: "acme", Auth: config.None},
		source: &fakeSource{repos: []catalog.Repo{{Name: "api"}}},
	}
	detail, err := listing(context.Background(), p)
	if err != nil {
		t.Fatalf("a real listing failed: %v", err)
	}
	if !strings.Contains(detail, "1 repositories") {
		t.Errorf("detail = %q", detail)
	}
}

// Only the local forge legitimately has nothing to clone.
func TestTheCloneCheckDistinguishesLocalFromABrokenRemote(t *testing.T) {
	local := &preflight{
		entry:  config.Workspace{Forge: config.Local, Dir: "/checkouts"},
		source: &fakeSource{},
		repos:  []catalog.Repo{{Name: "api", Dir: "/checkouts/api"}},
	}
	if _, err := cloneCheck(context.Background(), local); err != nil {
		t.Errorf("a local workspace failed the clone check: %v", err)
	}

	remote := &preflight{
		entry:  config.Workspace{Forge: config.GitHub, Slug: "acme"},
		source: &fakeSource{},
		repos:  []catalog.Repo{{Name: "api"}},
	}
	if _, err := cloneCheck(context.Background(), remote); err == nil {
		t.Error("a remote forge offering no clone url passed as a local checkout")
	}
}
