// Package catalog is the forge-agnostic vocabulary every other domain package
// speaks. It imports nothing and knows no API.
package catalog

// Signal gates the scan stage. Empty means the forge supplies none, and such a
// repository can never be proven untouched.
type Signal string

func (s Signal) Present() bool { return s != "" }

// Same reports whether two signals prove a repository untouched. An absent
// signal proves nothing, so it is never the same as anything - including
// another absent signal.
func (s Signal) Same(other Signal) bool { return s.Present() && s == other }

func (s Signal) String() string { return string(s) }

// Repo is one repository as the manifest reports it: everything a forge can say
// without anybody cloning anything.
type Repo struct {
	// Name is what the whole pipeline indexes a repository by, and what a
	// reader greps for. It is the forge's short name, not a URL.
	Name string
	// Summary is the forge's own one-line description, when it has one. It is a
	// prompt input, never the indexed sentence: it is written by whoever created
	// the repository and is empty or stale about as often as not.
	Summary string
	// Branch is the default branch. Empty lets the clone take the remote's.
	Branch string
	// Remote is where a clone comes from. Empty means the working tree is
	// already on disk and this repository is never cloned.
	Remote string
	// Dir pins a working tree the forge already knows the location of, which is
	// how the local forge reports a checkout that is not this tool's to manage.
	Dir string
	// Archived marks a repository the forge reports as read-only.
	Archived bool
	// Activity is the forge's last-activity stamp: the free signal that decides
	// whether this repository is scanned again.
	Activity Signal
}

// Key is the name the whole pipeline indexes a repository by.
func (r Repo) Key() string { return r.Name }

// Skip is one row of the skip table: a repository that is listed in the
// catalog but never scanned or described.
type Skip struct {
	Reason string
	When   func(Repo) bool
}

// Skips is the skip decision as data, in the order it is asked. Archived
// repositories are kept in the catalog rather than dropped, because "this
// exists and is frozen" is exactly the answer a reader looking for it needs;
// spending a model call to describe one is what is not worth doing.
var Skips = []Skip{
	{Reason: "archived", When: func(r Repo) bool { return r.Archived }},
}

// Skipped reports why a repository is not described, and whether it is.
func (r Repo) Skipped() (string, bool) {
	for _, skip := range Skips {
		if skip.When(r) {
			return skip.Reason, true
		}
	}
	return "", false
}

// Placeholder is what a skipped repository shows in place of a description: the
// reason in parentheses, so a reader sees why the row has no sentence.
func Placeholder(reason string) string { return "(" + reason + ")" }

// IsPlaceholder reports whether a description is a skip placeholder rather than
// a sentence. Only the reasons in Skips count, so a real sentence that happens
// to be parenthesised is not mistaken for one.
func IsPlaceholder(description string) bool {
	for _, skip := range Skips {
		if description == Placeholder(skip.Reason) {
			return true
		}
	}
	return false
}

// Describable reports whether this repository earns a generated sentence.
func (r Repo) Describable() bool {
	_, skipped := r.Skipped()
	return !skipped
}
