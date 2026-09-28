package fingerprint

import (
	"strings"
	"testing"

	"github.com/branow/repomap/internal/catalog"
)

func tree(paths ...string) catalog.Tree {
	t := catalog.Tree{Branch: "main", Readme: "# a readme"}
	for _, path := range paths {
		t.Files = append(t.Files, catalog.File{Path: path, Lines: 10})
	}
	return t
}

// TestHeadIsNotPartOfTheFingerprint is the whole reason this package exists. A
// commit that changes no file the index renders is not a change, and treating
// HEAD as one is the single-tier behaviour that buys a model call per push.
func TestHeadIsNotPartOfTheFingerprint(t *testing.T) {
	repo := catalog.Repo{Name: "api"}
	before := tree("main.go", "go.mod")
	before.Head = "aaaaaaaaaaaa"
	after := tree("main.go", "go.mod")
	after.Head = "bbbbbbbbbbbb"
	after.LastCommit = "2026-09-28"

	if Of(repo, before) != Of(repo, after) {
		t.Error("moving HEAD changed the fingerprint; every push would buy a description")
	}
	if strings.Contains(Canonical(repo, after), after.Head) {
		t.Error("the commit hash reached the canonical form")
	}
}

func TestWhatDoesChangeTheFingerprint(t *testing.T) {
	repo := catalog.Repo{Name: "api", Summary: "the api"}
	base := tree("main.go", "go.mod")

	tests := []struct {
		name   string
		repo   catalog.Repo
		tree   catalog.Tree
		differ bool
	}{
		{name: "a new file", tree: tree("main.go", "go.mod", "router.go"), differ: true},
		{name: "a removed file", tree: tree("main.go"), differ: true},
		{name: "a renamed file", tree: tree("app.go", "go.mod"), differ: true},
		{
			name: "a file that changed scale",
			tree: func() catalog.Tree {
				t := tree("main.go", "go.mod")
				t.Files[0].Lines = 900
				return t
			}(),
			differ: true,
		},
		{
			name: "a file edited by a few lines",
			tree: func() catalog.Tree {
				t := tree("main.go", "go.mod")
				t.Files[0].Lines = 13
				return t
			}(),
			differ: false,
		},
		{
			name: "a rewritten readme",
			tree: func() catalog.Tree {
				t := tree("main.go", "go.mod")
				t.Readme = "# something else entirely"
				return t
			}(),
			differ: true,
		},
		{
			name: "a readme reformatted to CRLF",
			tree: func() catalog.Tree {
				t := tree("main.go", "go.mod")
				t.Readme = "# a readme\r\n"
				return t
			}(),
			differ: false,
		},
		{name: "a changed forge summary", repo: catalog.Repo{Name: "api", Summary: "renamed"},
			tree: base, differ: true},
		{
			name: "a different default branch",
			tree: func() catalog.Tree {
				t := tree("main.go", "go.mod")
				t.Branch = "trunk"
				return t
			}(),
			differ: true,
		},
	}

	before := Of(repo, base)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject := tt.repo
			if subject.Name == "" {
				subject = repo
			}
			if (Of(subject, tt.tree) != before) != tt.differ {
				t.Errorf("fingerprint differs = %v, want %v", !tt.differ, tt.differ)
			}
		})
	}
}

// TestFileOrderDoesNotMove pins that the scanner's sort is load-bearing: a
// fingerprint that depended on the order git listed files in would redescribe
// every repository on a filesystem that enumerates differently.
func TestFileOrderDoesNotMove(t *testing.T) {
	repo := catalog.Repo{Name: "api"}
	one := tree("a.go", "b.go", "c.go")
	other := tree("a.go", "b.go", "c.go")
	other.Files[0], other.Files[2] = other.Files[2], other.Files[0]

	// The scanner sorts, so a caller handing them in unsorted is a caller bug -
	// but the canonical form must be readable enough to see that at a glance.
	if !strings.Contains(Canonical(repo, one), "f a.go") {
		t.Error("the canonical form is not one legible line per file")
	}
	if Of(repo, one) == Of(repo, other) {
		t.Error("the canonical form is order-insensitive, which hides a scanner that stopped sorting")
	}
}

func TestSizeBand(t *testing.T) {
	tests := []struct {
		before, after int
		same          bool
	}{
		{before: 40, after: 41, same: true},
		{before: 40, after: 900, same: false},
		{before: 0, after: 0, same: true},
		{before: 0, after: 1, same: true},
		{before: 1, after: 3, same: false},
		{before: 1000, after: 1010, same: true},
	}
	for _, tt := range tests {
		if (sizeBand(tt.before) == sizeBand(tt.after)) != tt.same {
			t.Errorf("sizeBand(%d) vs sizeBand(%d): same = %v, want %v",
				tt.before, tt.after, !tt.same, tt.same)
		}
	}
}

func TestNormalize(t *testing.T) {
	if Normalize("a\r\nb   \n\nc\n") != "a\nb\n\nc" {
		t.Errorf("normalize = %q", Normalize("a\r\nb   \n\nc\n"))
	}
}

func TestFingerprintIsShortEnoughForATSVRow(t *testing.T) {
	got := Of(catalog.Repo{Name: "api"}, tree("main.go"))
	if len(got) != digits {
		t.Errorf("fingerprint is %d characters, want %d", len(got), digits)
	}
}
