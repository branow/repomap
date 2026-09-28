package cmdutil

import (
	"context"
	"errors"

	"github.com/branow/llm"
	"github.com/branow/repomap/internal/clone"
	"github.com/branow/repomap/internal/config"
	"github.com/branow/repomap/internal/credentials"
	"github.com/branow/repomap/internal/forge"
	"github.com/branow/repomap/internal/forge/local"
	"github.com/branow/repomap/internal/git"
	"github.com/branow/repomap/internal/iostreams"
	"github.com/branow/repomap/internal/output"
	"github.com/branow/repomap/internal/scan"
)

// The exit table: this tool's contract with a script that calls it.
const (
	ExitOK          = 0
	ExitError       = 1
	ExitCancelled   = 2
	ExitValidation  = 3
	ExitAuth        = 4
	ExitNotFound    = 5
	ExitUnavailable = 6
)

// Code documents one exit status, so the docs, the mapping below and the
// reachability test read one list.
type Code struct {
	Value   int
	Name    string
	Meaning string
}

var table = []Code{
	{ExitOK, "ok", "the command did what it was asked"},
	{ExitError, "generic", "anything unclassified, including a defect in this tool"},
	{ExitCancelled, "cancelled", "the user aborted, or input ended at a prompt"},
	{ExitValidation, "validation", "the input was refused: bad value, missing value, " +
		"a target this tool will not read"},
	{ExitAuth, "auth", "a credential was presented and rejected, or could not be read"},
	{ExitNotFound, "not found", "a named entry, setting, account or stored secret " +
		"does not exist"},
	{ExitUnavailable, "unavailable", "a dependency could not be reached: keychain, " +
		"forge, git remote, model"},
}

func Codes() []Code {
	out := make([]Code, len(table))
	copy(out, table)
	return out
}

// rules map an error to an exit code, matched by errors.Is/As and never by
// message. The order is load-bearing: a wrapped cause is matched before its
// wrapper, so a refused credential inside a clone failure reports auth, not
// unavailable.
var rules = []struct {
	code  int
	match func(error) bool
}{
	{ExitCancelled, is(ErrCancelled)},
	{ExitCancelled, is(iostreams.ErrCancelled)},
	{ExitCancelled, is(context.Canceled)},

	{ExitValidation, is(iostreams.ErrNoInput)},
	{ExitValidation, as[*ValidationError]},
	{ExitValidation, as[*config.InvalidError]},
	{ExitValidation, as[*config.InUseError]},
	{ExitValidation, as[*output.UnknownFormatError]},
	{ExitValidation, as[*forge.UnsupportedError]},
	// A directory that is not there, or not readable, is the user's
	// configuration to fix rather than a forge that is down.
	{ExitValidation, as[*local.DirectoryError]},

	{ExitAuth, as[*AuthError]},
	{ExitAuth, as[*forge.AuthError]},
	{ExitAuth, llmClass(llm.ClassAuth)},

	{ExitNotFound, as[*NotConfiguredError]},
	{ExitNotFound, as[*config.NotFoundError]},
	{ExitNotFound, as[*credentials.NotFoundError]},
	{ExitNotFound, as[*forge.NotFoundError]},
	{ExitNotFound, as[*scan.EmptyError]},

	{ExitUnavailable, as[*UnavailableError]},
	{ExitUnavailable, as[*credentials.KeychainError]},
	{ExitUnavailable, as[*credentials.ReadOnlyError]},
	{ExitUnavailable, as[*forge.UnavailableError]},
	{ExitUnavailable, as[*forge.ResponseError]},
	{ExitUnavailable, as[*clone.AccessError]},
	{ExitUnavailable, as[*clone.CloneError]},
	{ExitUnavailable, as[*clone.UpdateError]},
	{ExitUnavailable, as[*git.MissingError]},
	{ExitUnavailable, llmClass(llm.ClassRateLimited)},
	{ExitUnavailable, llmClass(llm.ClassUnavailable)},
}

// ExitCode is the single translation from an error to a process exit status.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	for _, rule := range rules {
		if rule.match(err) {
			return rule.code
		}
	}
	return ExitError
}

func is(target error) func(error) bool {
	return func(err error) bool { return errors.Is(err, target) }
}

func as[T error](err error) bool {
	var target T
	return errors.As(err, &target)
}

// llmClass matches a provider failure by its class, never by its message: the
// llm module classifies these precisely so nothing above it has to guess from
// a provider's prose.
func llmClass(class llm.Class) func(error) bool {
	return func(err error) bool {
		var target *llm.Error
		return errors.As(err, &target) && target.Class == class
	}
}
