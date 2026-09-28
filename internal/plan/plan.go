// Package plan turns a fresh manifest plus the previously written index into
// the two work sets a build runs: Scan, decided by the forge's activity signal,
// and Describe, decided by content fingerprint. Both are pure; the scan stage
// runs between them.
package plan

import (
	"sort"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/fingerprint"
)

// Reason records why a repository is being scanned again.
type Reason string

const (
	// New is a repository the previous build never saw.
	New Reason = "new"
	// Moved covers a moved activity signal and an absent one: a repository with
	// no signal can never be proven untouched, so it always rescans.
	Moved Reason = "moved"
	// Undescribed is a repository whose row was written without a sentence,
	// which is what a failed describe leaves behind. Without this rule its
	// signal would match next run and the row would stay empty forever.
	Undescribed Reason = "undescribed"
)

// Rule is one row of the scan table.
type Rule struct {
	Reason Reason
	// When is asked only after every earlier rule declined, so every rule but
	// the first may assume prior is non-nil.
	When func(repo catalog.Repo, prior *catalog.State) bool
}

// ScanReasons is the scan decision as data, in the order it is asked; the
// first rule that fires names the reason.
var ScanReasons = []Rule{
	{
		Reason: New,
		When:   func(_ catalog.Repo, prior *catalog.State) bool { return prior == nil },
	},
	{
		Reason: Moved,
		When: func(repo catalog.Repo, prior *catalog.State) bool {
			return !repo.Activity.Same(prior.Activity)
		},
	},
	{
		Reason: Undescribed,
		When: func(repo catalog.Repo, prior *catalog.State) bool {
			return repo.Describable() && prior.Description == ""
		},
	},
}

// ScanPlan is what the scan stage is asked to do, and what it may skip.
type ScanPlan struct {
	Scan  []catalog.Repo
	Reuse []catalog.Repo
	// Skipped are repositories the catalog lists but never scans, each paired
	// with the rule that excused it.
	Skipped []catalog.Repo
	// Dropped are names the forge no longer has, sorted, so the writer can
	// remove their rows.
	Dropped []string
	// Reasons maps a repository name to why it is being scanned.
	Reasons map[string]Reason
}

// Scan decides what to read from disk, given the state the previous build left
// in the index it wrote.
func Scan(repos []catalog.Repo, state map[string]catalog.State) ScanPlan {
	plan := ScanPlan{Reasons: make(map[string]Reason)}
	seen := make(map[string]struct{}, len(repos))

	for _, repo := range repos {
		key := repo.Key()
		seen[key] = struct{}{}
		if _, skipped := repo.Skipped(); skipped {
			plan.Skipped = append(plan.Skipped, repo)
			continue
		}
		var prior *catalog.State
		if recorded, ok := state[key]; ok {
			prior = &recorded
		}
		if reason, hit := reasonFor(repo, prior); hit {
			plan.Reasons[key] = reason
			plan.Scan = append(plan.Scan, repo)
			continue
		}
		plan.Reuse = append(plan.Reuse, repo)
	}

	for key := range state {
		if _, ok := seen[key]; !ok {
			plan.Dropped = append(plan.Dropped, key)
		}
	}
	sort.Strings(plan.Dropped)
	return plan
}

func reasonFor(repo catalog.Repo, prior *catalog.State) (Reason, bool) {
	for _, rule := range ScanReasons {
		if rule.When(repo, prior) {
			return rule.Reason, true
		}
	}
	return "", false
}

// DescribePlan is what the model is asked to look at, and what it is not.
type DescribePlan struct {
	Describe []catalog.Entry
	// Unchanged keep the description already written for them, and their
	// per-file phrases with it.
	Unchanged []catalog.Entry
}

// Describe splits freshly scanned entries by content fingerprint, each returned
// carrying the hash just computed. An entry whose hash matches but has no prior
// description to reuse is still described.
func Describe(scanned []catalog.Entry, state map[string]catalog.State) DescribePlan {
	var plan DescribePlan

	for _, entry := range scanned {
		entry.Fingerprint = fingerprint.Of(entry.Repo, entry.Tree)

		prior, ok := state[entry.Key()]
		if ok && prior.Fingerprint == entry.Fingerprint && prior.Description != "" {
			entry.Description = prior.Description
			entry.Tree.Files = restore(entry.Tree.Files, prior.Files)
			plan.Unchanged = append(plan.Unchanged, entry)
			continue
		}
		plan.Describe = append(plan.Describe, entry)
	}

	return plan
}

// restore puts the previous build's per-file phrases back onto a freshly
// scanned file list. The fingerprint has already said the inventory did not
// change, so a phrase that finds no file is a phrase for a file that was
// removed, and is dropped with it.
func restore(files []catalog.File, phrases map[string]string) []catalog.File {
	if len(phrases) == 0 {
		return files
	}
	out := make([]catalog.File, len(files))
	copy(out, files)
	for i := range out {
		if phrase, ok := phrases[out[i].Path]; ok {
			out[i].Purpose = phrase
		}
	}
	return out
}

// Summary is the one-line shape of a plan, for a build's progress output.
type Summary struct {
	Scan      int
	Reuse     int
	Skipped   int
	Dropped   int
	Describe  int
	Unchanged int
}

// Summarize counts both plans together.
func Summarize(scan ScanPlan, describe DescribePlan) Summary {
	return Summary{
		Scan:      len(scan.Scan),
		Reuse:     len(scan.Reuse),
		Skipped:   len(scan.Skipped),
		Dropped:   len(scan.Dropped),
		Describe:  len(describe.Describe),
		Unchanged: len(describe.Unchanged),
	}
}
