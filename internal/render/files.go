package render

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/branow/repomap/internal/catalog"
)

// filesHeader is the per-repository inventory's columns.
var filesHeader = []string{"path", "lines", "size", "purpose"}

// Files writes one repository's inventory: every tracked path, with a phrase on
// the ones whose purpose the path does not give away.
//
// Every path is written, not just the annotated ones. A phrase per file is what
// the model was asked for and what it charges for; the inventory is free, and a
// reader looking for "where is the router configured" needs the paths that were
// never worth a phrase just as much as the ones that were.
func Files(dir string, entry catalog.Entry) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}

	rows := make([][]string, 0, len(entry.Tree.Files))
	for _, file := range entry.Tree.Files {
		rows = append(rows, []string{
			file.Path,
			strconv.Itoa(file.Lines),
			Size(file.Bytes),
			file.Purpose,
		})
	}

	// The header carries the fingerprint the rows were written at, so a reader
	// - and the next build - can tell which state this file describes without
	// opening the catalog beside it.
	var head strings.Builder
	head.WriteString("# " + entry.Key() + " @" + entry.Fingerprint + "\n")
	head.WriteString("# " + strconv.Itoa(len(entry.Tree.Files)) + " files  " +
		Size(entry.Tree.Bytes))
	if entry.Tree.LastCommit != "" {
		head.WriteString("  last commit " + entry.Tree.LastCommit)
	}
	if entry.Tree.Branch != "" {
		head.WriteString("  branch " + entry.Tree.Branch)
	}
	head.WriteString("\n")
	if entry.Description != "" {
		head.WriteString("# " + clean(entry.Description) + "\n")
	}

	path := filepath.Join(dir, entry.Key()+".tsv")
	return write(path, head.String()+TSV(filesHeader, rows))
}

// Prune removes the inventory files of repositories the forge no longer has, so
// the tree does not accumulate files for repositories that are gone.
func Prune(dir string, dropped []string) error {
	for _, name := range dropped {
		if err := os.Remove(filepath.Join(dir, name+".tsv")); err != nil &&
			!os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
