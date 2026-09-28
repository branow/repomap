package describe

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/branow/llm"
	"github.com/branow/repomap/internal/catalog"
)

// fake is an in-memory provider. No test in this package opens a socket or
// starts a process.
type fake struct {
	mu       sync.Mutex
	calls    int
	answer   func(prompt string) (Answer, error)
	inFlight int
	peak     int
}

func (f *fake) Complete(ctx context.Context, request llm.Request) (*llm.Response, error) {
	f.mu.Lock()
	f.calls++
	f.inFlight++
	if f.inFlight > f.peak {
		f.peak = f.inFlight
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()

	answer, err := f.answer(request.Prompt)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(answer)
	return &llm.Response{Structured: body, Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}}, nil
}

func (f *fake) Name() string { return "fake" }

func entry(name string, paths ...string) catalog.Entry {
	tree := catalog.Tree{Branch: "main", Readme: "# " + name}
	for _, path := range paths {
		tree.Files = append(tree.Files, catalog.File{Path: path, Lines: 10})
	}
	return catalog.Entry{Repo: catalog.Repo{Name: name}, Tree: tree}
}

func sentences(answer string) func(string) (Answer, error) {
	return func(string) (Answer, error) { return Answer{Sentence: answer}, nil }
}

// TestTheOutputFieldIsNeverCalledDescription pins the prompt lesson that cost
// real debugging: a field named `description` gets filled with a description OF
// THE FIELD, and better instruction-following makes it more reliably wrong.
func TestTheOutputFieldIsNeverCalledDescription(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(Schema, &schema); err != nil {
		t.Fatal(err)
	}
	if _, wrong := schema.Properties["description"]; wrong {
		t.Error("the answer field is named `description`; every model fills that with " +
			"a description of the field")
	}
	if _, right := schema.Properties["sentence"]; !right {
		t.Error("the answer field is no longer named `sentence`")
	}
	// The schema text has to read as an order, not as a noun phrase.
	if !strings.Contains(string(Schema), "verbatim") {
		t.Error("the schema text no longer instructs the model to put the finished text in")
	}
}

func TestThePromptCarriesWhatTheFingerprintCovers(t *testing.T) {
	e := entry("api", "main.go", "internal/router.go")
	e.Repo.Summary = "the order api"
	prompt := Prompt(e)

	for _, want := range []string{"api", "the order api", "main.go", "internal/router.go", "# api"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt never mentions %q", want)
		}
	}
}

// TestALongFileListIsTruncatedNotDropped: a repository over the path cap still
// gets its sentence.
func TestALongFileListIsTruncatedNotDropped(t *testing.T) {
	paths := make([]string, MaxPaths+50)
	for i := range paths {
		paths[i] = "src/file" + strings.Repeat("x", i%5) + string(rune('a'+i%26)) + ".go"
	}
	prompt := Prompt(entry("big", paths...))
	if !strings.Contains(prompt, "and 50 more") {
		t.Error("a file list past the cap was not reported as truncated")
	}
}

// TestAnswersAreKeyedByWhatWasAsked, never by what the model echoed back. Filing
// a sentence under the name a model returned is how an object counts as
// described while itself reporting missing.
func TestAnswersAreKeyedByWhatWasAsked(t *testing.T) {
	client := &fake{answer: func(string) (Answer, error) {
		return Answer{Sentence: "a thing"}, nil
	}}
	result, err := Run(context.Background(), client,
		[]catalog.Entry{entry("api", "main.go")}, Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Answers["api"]; !ok {
		t.Errorf("answers keyed by %v, want api", keysOf(result.Answers))
	}
}

// TestOneFailureDoesNotDiscardTheRest: a run that abandoned everything because
// one provider call failed would throw away every sentence it had already paid
// for.
func TestOneFailureDoesNotDiscardTheRest(t *testing.T) {
	client := &fake{answer: func(prompt string) (Answer, error) {
		if strings.Contains(prompt, "Repository: broken") {
			return Answer{}, errors.New("provider said no")
		}
		return Answer{Sentence: "a thing"}, nil
	}}

	result, err := Run(context.Background(), client, []catalog.Entry{
		entry("api", "main.go"),
		entry("broken", "main.go"),
		entry("web", "app.tsx"),
	}, Options{Model: "m", Workers: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Answers) != 2 {
		t.Errorf("kept %d answers, want 2", len(result.Answers))
	}
	if _, failed := result.Failed["broken"]; !failed {
		t.Error("the failing repository is not reported as failed")
	}
}

// TestAnEmptySentenceIsReportedNotStored: a backend that answers every call and
// says nothing leaves the same empty column as a failure, with no failure to
// show for it.
func TestAnEmptySentenceIsReportedNotStored(t *testing.T) {
	client := &fake{answer: sentences("   ")}
	result, err := Run(context.Background(), client,
		[]catalog.Entry{entry("api", "main.go")}, Options{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "api" {
		t.Errorf("missing = %v, want [api]", result.Missing)
	}
	if len(result.Answers) != 0 {
		t.Error("a blank sentence was stored as an answer")
	}
}

// TestConcurrencyIsBoundedAndTheResultIsNot: repositories are described several
// at a time, and the order results merge in does not depend on which call
// finished first.
func TestConcurrencyIsBoundedAndTheResultIsNot(t *testing.T) {
	client := &fake{answer: func(prompt string) (Answer, error) {
		return Answer{Sentence: "a thing"}, nil
	}}
	entries := make([]catalog.Entry, 12)
	for i := range entries {
		entries[i] = entry("repo" + string(rune('a'+i)))
	}

	result, err := Run(context.Background(), client, entries, Options{Model: "m", Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Answers) != len(entries) {
		t.Errorf("answered %d of %d", len(result.Answers), len(entries))
	}
	if client.peak > 4 {
		t.Errorf("%d calls were in flight at once, want at most 4", client.peak)
	}
	if result.Usage.InputTokens != 10*len(entries) {
		t.Errorf("usage = %+v, want every call counted", result.Usage)
	}
}

func TestNothingToDescribeMakesNoCall(t *testing.T) {
	client := &fake{answer: sentences("a thing")}
	if _, err := Run(context.Background(), client, nil, Options{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if client.calls != 0 {
		t.Errorf("%d calls made for an empty work set", client.calls)
	}
}

// TestApplyDropsAPathThatMatchesNothing: a phrase attached to no file is a row
// the reader cannot check, so a model that invents or reformats a path gets its
// phrase dropped rather than a row invented for it.
func TestApplyDropsAPathThatMatchesNothing(t *testing.T) {
	e := entry("api", "main.go", "router.go")
	applied := Apply(e, Answer{
		Sentence: "the api",
		Files: []FileAnswer{
			{Path: "main.go", Purpose: "process entry point"},
			{Path: "./main.go", Purpose: "reformatted"},
			{Path: "imaginary.go", Purpose: "never existed"},
			{Path: "router.go", Purpose: "   "},
		},
	})

	if len(applied.Tree.Files) != 2 {
		t.Fatalf("the file list grew to %d", len(applied.Tree.Files))
	}
	if applied.Tree.Files[0].Purpose != "process entry point" {
		t.Errorf("main.go purpose = %q", applied.Tree.Files[0].Purpose)
	}
	if applied.Tree.Files[1].Purpose != "" {
		t.Errorf("a blank phrase was stored: %q", applied.Tree.Files[1].Purpose)
	}
	if applied.Description != "the api" {
		t.Errorf("description = %q", applied.Description)
	}
}

// TestApplyDoesNotMutateItsInput: entries are carried by value through the
// pipeline, and a shared backing array would let one repository's phrases land
// on another's rows.
func TestApplyDoesNotMutateItsInput(t *testing.T) {
	e := entry("api", "main.go")
	Apply(e, Answer{Sentence: "x", Files: []FileAnswer{{Path: "main.go", Purpose: "written"}}})

	if e.Tree.Files[0].Purpose != "" {
		t.Error("Apply wrote through to its caller's file list")
	}
}

func TestPlural(t *testing.T) {
	if got := Plural(1, "repository", "repositories"); got != "1 repository" {
		t.Errorf("Plural = %q", got)
	}
	if got := Plural(3, "repository", "repositories"); got != "3 repositories" {
		t.Errorf("Plural = %q", got)
	}
}

func keysOf(m map[string]Answer) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
