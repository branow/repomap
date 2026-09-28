// Package fingerprint decides what "changed" means for a repository. It is the
// second of the two staleness tiers: the forge's activity stamp decides whether
// a repository is scanned, and this decides whether the scan was worth a model
// call.
//
// The distinction is the whole economy of the tool. A repository with a commit
// a day moves its activity stamp every run and would buy a description every
// run; almost none of those commits change what one sentence about the
// repository, or a phrase about a file, would say.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode"

	"github.com/branow/repomap/internal/catalog"
)

// digits keeps 64 bits of the hash, short enough to sit in a TSV row.
const digits = 16

// Sum is the hash every fingerprint here is taken with.
func Sum(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:digits]
}

// Normalize strips line endings and trailing whitespace, so a checkout on a
// machine configured for CRLF does not read as an edit of every file.
func Normalize(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Canonical is the exact text a repository's fingerprint is taken over. Written
// out line by line so adding a field to catalog.Tree cannot move the hash.
//
// Everything the describer sees appears here. A fact the model reads but the
// hash does not is a fact that changes the answer without earning a fresh one.
// The commit hash is deliberately absent: HEAD moves on every commit, including
// the ones inside a file the index names but never reads.
func Canonical(repo catalog.Repo, tree catalog.Tree) string {
	lines := make([]string, 0, len(tree.Files)+3)
	lines = append(lines, "summary "+Normalize(repo.Summary))
	lines = append(lines, "branch "+tree.Branch)
	for _, file := range tree.Files {
		// The line count travels with the path: a file that tripled in size is
		// a file whose purpose may have changed, and it costs nothing to notice.
		lines = append(lines, "f "+file.Path+" "+strconv.Itoa(sizeBand(file.Lines)))
	}
	lines = append(lines, "readme "+Sum(Normalize(tree.Readme)))
	return strings.Join(lines, "\n")
}

// sizeBand buckets a line count so ordinary editing does not move the hash while
// a file changing scale does. A file going from 40 lines to 41 is the same file;
// one going from 40 to 900 is not the thing the last description described.
//
// The bands are powers of two because growth that matters here is
// multiplicative: the gap between 10 and 20 lines means something, the gap
// between 1010 and 1020 does not.
func sizeBand(lines int) int {
	band := 0
	for n := lines; n > 1; n /= 2 {
		band++
	}
	return band
}

// Of returns the content fingerprint for one repository.
func Of(repo catalog.Repo, tree catalog.Tree) string {
	return Sum(Canonical(repo, tree))
}
