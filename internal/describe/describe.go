// Package describe turns scanned repositories into sentences. It is the only
// stage that spends money, and the only one whose output is not derivable from
// what is on disk.
package describe

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/branow/llm"
	"github.com/branow/repomap/internal/catalog"
)

// DefaultWorkers is one: a library caller gets deterministic ordering unless it
// asks otherwise. The command sets its own ceiling, because describing is the
// long pole of a build and every repository is independent.
const DefaultWorkers = 1

// One call per repository, never a batch.
//
// dbmap batches several database objects into one call, and it is right to: a
// two-column lookup table's prompt is a few hundred characters, so a call per
// object would be a thousand calls buying a thousand sentences. A repository's
// prompt is its whole file list. Two of them in one call is a prompt the model
// answers worse, for a saving of one request - and a failure then loses both.
// The concurrency below is where the wall-clock saving actually is.

// Result is what a run produced.
type Result struct {
	// Answers is keyed by the REQUESTED repository name, never by anything the
	// model echoed back.
	Answers map[string]Answer
	// Missing lists repositories the model skipped or refused, reported rather
	// than left blank.
	Missing []string
	// Failed lists repositories whose call errored. A failure is per
	// repository, never fatal: one repository the provider choked on must not
	// discard the other two hundred a run already paid for.
	Failed map[string]error
	Usage  llm.Usage
}

// Logger reports progress and what was skipped.
type Logger interface {
	Info(message string)
	Warn(message string)
}

// Options configure one describe run.
type Options struct {
	Model string
	// Workers is how many repositories may be in flight. 0 means
	// DefaultWorkers.
	Workers int
	Logger  Logger
}

func (o Options) workers() int {
	if o.Workers <= 0 {
		return DefaultWorkers
	}
	return o.Workers
}

// Run describes every entry, concurrently, and returns the answers keyed by
// repository name. The order results are merged in does not depend on which
// call finished first.
func Run(ctx context.Context, client llm.Client, entries []catalog.Entry,
	opts Options) (Result, error) {

	result := Result{
		Answers: make(map[string]Answer, len(entries)),
		Failed:  make(map[string]error),
	}
	if len(entries) == 0 {
		return result, nil
	}

	type outcome struct {
		key    string
		answer Answer
		usage  llm.Usage
		err    error
		empty  bool
	}
	outcomes := make([]outcome, len(entries))

	workers := opts.workers()
	if workers > len(entries) {
		workers = len(entries)
	}
	var next int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= len(entries) {
					return
				}
				if ctx.Err() != nil {
					return
				}
				entry := entries[i]
				answer, usage, err := one(ctx, client, entry, opts.Model)
				outcomes[i] = outcome{
					key:    entry.Key(),
					answer: answer,
					usage:  usage,
					err:    err,
					empty:  err == nil && strings.TrimSpace(answer.Sentence) == "",
				}
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return result, err
	}

	for _, o := range outcomes {
		if o.key == "" {
			continue
		}
		result.Usage.Add(o.usage)
		switch {
		case o.err != nil:
			result.Failed[o.key] = o.err
			warn(opts.Logger, "describe: "+o.key+" failed: "+o.err.Error())
		case o.empty:
			result.Missing = append(result.Missing, o.key)
			warn(opts.Logger, "describe: "+o.key+" came back with no sentence")
		default:
			result.Answers[o.key] = o.answer
			info(opts.Logger, "describe: "+o.key+"  "+o.answer.Sentence)
		}
	}
	return result, nil
}

// one describes a single repository. The answer is filed under the key that was
// ASKED about, so a model that renames the repository in its reply cannot file
// a sentence under a name nothing requested - the failure mode where an object
// counts as described while itself reporting missing.
func one(ctx context.Context, client llm.Client, entry catalog.Entry,
	model string) (Answer, llm.Usage, error) {

	response, err := client.Complete(ctx, llm.Request{
		System: System,
		Prompt: Prompt(entry),
		Schema: Schema,
		Model:  model,
	})
	if err != nil {
		return Answer{}, llm.Usage{}, err
	}

	var answer Answer
	if err := json.Unmarshal(response.Structured, &answer); err != nil {
		return Answer{}, response.Usage, err
	}
	answer.Sentence = strings.TrimSpace(answer.Sentence)
	return answer, response.Usage, nil
}

// Apply writes an answer's phrases onto an entry's file list, matching by path.
// A path the model invented - a file it thought ought to exist, or one it
// reformatted - matches nothing and is dropped, because a phrase attached to no
// file is a row the reader cannot check.
func Apply(entry catalog.Entry, answer Answer) catalog.Entry {
	phrases := make(map[string]string, len(answer.Files))
	for _, file := range answer.Files {
		if phrase := strings.TrimSpace(file.Purpose); phrase != "" {
			phrases[file.Path] = phrase
		}
	}
	files := make([]catalog.File, len(entry.Tree.Files))
	copy(files, entry.Tree.Files)
	for i := range files {
		if phrase, ok := phrases[files[i].Path]; ok {
			files[i].Purpose = phrase
		}
	}
	entry.Tree.Files = files
	entry.Description = answer.Sentence
	return entry
}

// Plural renders a count with its noun. Both forms are passed rather than an
// "s" being appended, because the noun this tool counts most often is
// "repository".
func Plural(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}

func info(logger Logger, message string) {
	if logger != nil {
		logger.Info(message)
	}
}

func warn(logger Logger, message string) {
	if logger != nil {
		logger.Warn(message)
	}
}
