package render

import (
	"os"
	"path/filepath"
	"strings"
)

// layout renders the file tree, with the gloss on each line aligned by
// measuring the name rather than by hand-typed padding - the names are
// constants, and a rename would otherwise leave this block ragged.
func layout() string {
	rows := [][2]string{
		{"    " + RosterFile, "one row per workspace: forge, counts, when it was built"},
		{"    <workspace>/", ""},
		{"      " + CatalogFile, "one row per repository, with a sentence saying what it is"},
		{"      " + ActiveFile, "the same rows, minus repositories with no commit in a year"},
		{"      " + FilesDir + "/<repo>.tsv",
			"one per repository: every tracked file, some with a phrase"},
	}
	widest := 0
	for _, row := range rows {
		if len(row[0]) > widest {
			widest = len(row[0])
		}
	}
	var out strings.Builder
	for _, row := range rows {
		out.WriteString(row[0])
		if row[1] != "" {
			out.WriteString(strings.Repeat(" ", widest-len(row[0])+2) + row[1])
		}
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

// guide is the tree's own format reference, written at the root of every index.
//
// It is addressed to whoever opens the tree with no tool installed, which is
// most of the people who will ever read one: it is committed with a project, or
// pulled from a repository somebody else builds into, and reading it needs
// nothing. A tree that could only be interpreted by consulting this project's
// documentation would be a tree that stops working the day that page moves.
var guide = `# Repository index

A map of a set of repositories: what each one is, and what each file in it is
for. Written by ` + "`repomap`" + `, read by anything.

Everything here is tab-separated plain text. There is no database and no
parser: open a file, or grep it.

## Layout

` + layout() + `

## Finding the right repository

Read ` + "`<workspace>/" + ActiveFile + "`" + ` whole. It is a list of
one-sentence descriptions, which is how you find a repository whose name you do
not know - grepping it only finds the ones you could already name. Fall back to
` + "`" + CatalogFile + "`" + ` when what you want is old or archived.

    repo                 files  size    commit      activity              fingerprint       description
    order-service        412    18 MB   2026-09-11  2026-09-11T08:22:01Z  8a928f2e9dabdf50  Places and tracks orders, Java and Spring Boot, publishing to Kafka.

` + "`activity`" + ` and ` + "`fingerprint`" + ` are the build's own bookkeeping.
Ignore them.

A description in parentheses - ` + "`(archived)`" + ` - is not a description: it
says the repository was listed but deliberately not described.

## Finding the right file

Read ` + "`<workspace>/" + FilesDir + "/<repo>.tsv`" + ` whole.

    # order-service @8a928f2e9dabdf50
    # 412 files  18 MB  last commit 2026-09-11  branch main
    # Places and tracks orders, Java and Spring Boot, publishing to Kafka.
    path                                   lines  size    purpose
    src/main/java/.../OrderController.java 240    9 KB    REST entry points for orders
    src/main/resources/application.yml     61     2 KB

Every tracked file is listed. A row with no phrase is a file whose path already
says what it is - that is the common case, and not a gap.

## What this is not

This is structure, not content. It says which repository and which file, so you
can go and read the code. It does not say what a function does, and it is only
as current as the ` + "`built`" + ` stamp in ` + RosterFile + `.
`

// WriteGuide writes the reader's format reference at the index root.
func WriteGuide(root string) error {
	if err := os.MkdirAll(root, dirMode); err != nil {
		return err
	}
	return write(filepath.Join(root, GuideFile), guide)
}
