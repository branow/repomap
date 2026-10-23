package cmd

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/branow/repomap/internal/cmdutil"
	"github.com/branow/repomap/internal/iostreams"
)

// documented pulls the flags out of a fenced block in the CLI reference. The
// block is the thing a reader copies from, so it is the thing worth checking.
func documented(t *testing.T, heading string) map[string]bool {
	t.Helper()
	body, err := os.ReadFile("../docs/CLI.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	at := strings.Index(text, heading)
	if at < 0 {
		t.Fatalf("docs/CLI.md no longer contains %q", heading)
	}
	// The fence the heading introduces, up to its close.
	rest := text[at:]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatalf("%q is not inside a fenced block", heading)
	}

	found := map[string]bool{}
	for _, m := range regexp.MustCompile(`--([a-z][a-z-]*)`).FindAllStringSubmatch(rest[:end], -1) {
		found[m[1]] = true
	}
	return found
}

// TestEveryIndexFlagIsDocumented is the enforcement behind the documentation
// rule in AGENTS.md. A flag added without a line in the reference is invisible,
// and a flag documented without existing is worse - this repository shipped a
// build skill telling an agent to pass `--clones` three commits before the flag
// was written.
func TestEveryIndexFlagIsDocumented(t *testing.T) {
	docs := documented(t, "repomap index [workspace] [flags]")

	var real []string
	newIndex(&cmdutil.Factory{}).Flags().VisitAll(func(f *pflag.Flag) {
		real = append(real, f.Name)
	})
	sort.Strings(real)

	for _, name := range real {
		if !docs[name] {
			t.Errorf("--%s exists and docs/CLI.md does not mention it", name)
		}
	}
	for name := range docs {
		if !contains(real, name) {
			t.Errorf("docs/CLI.md documents --%s, which the index command does not have", name)
		}
	}
}

// TestThePersistentFlagsAreDocumented covers the ones every command carries, in
// whatever section describes them.
func TestThePersistentFlagsAreDocumented(t *testing.T) {
	body, err := os.ReadFile("../docs/CLI.md")
	if err != nil {
		t.Fatal(err)
	}
	reference := string(body)

	io, _, _, _ := iostreams.Test()
	NewRoot(&cmdutil.Factory{IO: io}, Build{}).PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if !strings.Contains(reference, "--"+f.Name) {
			t.Errorf("the persistent --%s is nowhere in docs/CLI.md", f.Name)
		}
	})
}

// TestEveryWorkspaceFlagIsDocumented: `workspace add` is where a workspace's
// whole shape is set, and its flags are documented in prose as much as in the
// example block, so any mention in the section counts.
func TestEveryWorkspaceFlagIsDocumented(t *testing.T) {
	body, err := os.ReadFile("../docs/CLI.md")
	if err != nil {
		t.Fatal(err)
	}
	const heading = "## Workspaces and backends"
	section := string(body)
	at := strings.Index(section, heading)
	if at < 0 {
		t.Fatalf("docs/CLI.md no longer contains %q", heading)
	}
	section = section[at+len(heading):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}

	newWorkspaceAdd(&cmdutil.Factory{}).Flags().VisitAll(func(f *pflag.Flag) {
		if !strings.Contains(section, "--"+f.Name) {
			t.Errorf("workspace add has --%s and %q does not mention it", f.Name, heading)
		}
	})
}

// TestTheSummaryColumnsAreDocumented: a run's summary is the only thing most
// people read, and three of its columns once existed solely in the source.
func TestTheSummaryColumnsAreDocumented(t *testing.T) {
	body, err := os.ReadFile("../docs/CLI.md")
	if err != nil {
		t.Fatal(err)
	}
	reference := string(body)

	source, err := os.ReadFile("index.go")
	if err != nil {
		t.Fatal(err)
	}
	named := regexp.MustCompile(`output\.Field\{Name: "([a-z ]+)"`).FindAllStringSubmatch(string(source), -1)
	if len(named) == 0 {
		t.Fatal("found no summary columns to check; this test has stopped testing anything")
	}

	for _, m := range named {
		column := m[1]
		// The example output is indented in a fenced block, so a column that is
		// reported appears at the start of one of its lines.
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(column) + `\s`).MatchString(reference) {
			t.Errorf("the summary reports %q and no example in docs/CLI.md shows it", column)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
