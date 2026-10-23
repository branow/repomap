package render

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/branow/repomap/internal/catalog"
)

// CatalogFile is the workspace's index: one row per repository, read whole by
// anyone looking for the right one.
const CatalogFile = "repos.tsv"

// ActiveFile is the same catalog with the long-dead repositories left out. It is
// a VIEW, not state: it carries no fingerprint, is never read back by a build,
// and is rewritten from the catalog every run.
//
// It exists for the reader alone. Finding the right repository means reading the
// catalog whole, and on an old account most of those rows are for repositories
// nobody has touched since. A reader who opens this one reads a third of the
// bytes and finds the same answer.
const ActiveFile = "repos-active.tsv"

// FilesDir holds one file per repository: its full inventory.
const FilesDir = "files"

// catalogHeader is the catalog's columns. `activity` and `fingerprint` are
// build state living in the catalog rather than in a sidecar, because a
// parallel state file drifts out of step with the index beside it.
// `remote` is here so the tree is enough to REACH the code it maps. A reader who
// pulled a published index has no forge credential and no binary, and without
// the clone url the index can say which file to open and still leave them with
// no way to open it.
var catalogHeader = []string{
	"repo", "files", "size", "commit", "remote", "activity", "fingerprint", "description",
}

// Catalog writes the workspace catalog, MERGING entries into whatever the file
// already holds. A narrowed run (--match, --limit) covers only what it selected,
// so rewriting from that selection would delete the fingerprint of every
// repository the run never looked at and make the next full build buy every
// description again. A row is replaced when this run covered it, removed when
// the forge no longer has it, and otherwise left exactly as it was.
func Catalog(dir string, entries []catalog.Entry, dropped []string) (int, error) {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return 0, err
	}
	path := filepath.Join(dir, CatalogFile)

	table, err := readCatalog(path)
	if err != nil {
		return 0, err
	}
	rows := migrate(table)
	for _, entry := range entries {
		rows[entry.Key()] = row(entry, rows[entry.Key()], table)
	}
	for _, key := range dropped {
		delete(rows, key)
	}

	// Rows are written sorted by name, so the file does not depend on which
	// repositories a run happened to select.
	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([][]string, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, rows[name])
	}

	if err := write(path, TSV(catalogHeader, ordered)); err != nil {
		return 0, err
	}
	return len(ordered), nil
}

// migrate rewrites every row a merge carries forward into this build's column
// order, so one build is enough to bring an older tree current rather than
// leaving a file whose rows disagree about what their columns mean.
func migrate(table Table) map[string][]string {
	rows := make(map[string][]string, len(table.Rows))
	for name, cells := range table.Rows {
		if len(table.Header) == len(catalogHeader) && table.At(cells, "remote") != "" {
			rows[name] = cells
			continue
		}
		current := make([]string, len(catalogHeader))
		for i, column := range catalogHeader {
			current[i] = table.At(cells, column)
		}
		current[0] = name
		rows[name] = current
	}
	return rows
}

// row renders one entry, carrying forward the columns only a scan can produce
// when this run did not scan this repository.
//
// A repository the activity signal proved untouched, and one skipped as
// archived, both arrive with no tree - there was nothing to read. Rendering
// their file count, size and last commit from that empty tree wrote 0, 0 B and
// an empty date over three columns that were correct, on every merge. The empty
// date was the worst of the three: an undated row is deliberately kept in the
// active view, so the view quietly filled up with repositories nobody had
// touched in four years.
func row(entry catalog.Entry, prior []string, table Table) []string {
	description := entry.Description
	if entry.Skip != "" {
		// A skipped repository says so in the column a reader looks at, rather
		// than leaving a blank that is indistinguishable from a failed describe.
		description = catalog.Placeholder(entry.Skip)
	}

	files := strconv.Itoa(len(entry.Tree.Files))
	size := Size(entry.Tree.Bytes)
	commit := entry.Tree.LastCommit
	if !entry.Scanned() && prior != nil {
		files = table.At(prior, "files")
		size = table.At(prior, "size")
		commit = table.At(prior, "commit")
	}

	return []string{
		entry.Key(),
		files,
		size,
		commit,
		entry.Repo.Remote,
		entry.Repo.Activity.String(),
		entry.Fingerprint,
		description,
	}
}

// FileTotal sums the file counts the catalog records, so the roster reports the
// whole workspace rather than whatever one run happened to scan.
func FileTotal(dir string) (int, error) {
	table, err := readCatalog(filepath.Join(dir, CatalogFile))
	if err != nil {
		return 0, err
	}
	total := 0
	for _, cells := range table.Rows {
		n, _ := strconv.Atoi(table.At(cells, "files"))
		total += n
	}
	return total, nil
}

// State reads back what the previous build recorded, from the files it wrote.
// A tree that has never been built returns an empty state and no error: a first
// build is not a failure.
//
// Columns are read BY NAME. Adding one shifted every position after it, and a
// build reading an older tree by position found no description on any row - so
// the whole account looked undescribed and every repository was bought again. A
// header lookup makes that class of mistake impossible instead of careful.
func State(dir string) (map[string]catalog.State, error) {
	table, err := readCatalog(filepath.Join(dir, CatalogFile))
	if err != nil {
		return nil, err
	}
	state := make(map[string]catalog.State, len(table.Rows))
	for name, cells := range table.Rows {
		entry := catalog.State{
			Remote:      table.At(cells, "remote"),
			Activity:    catalog.Signal(table.At(cells, "activity")),
			Fingerprint: table.At(cells, "fingerprint"),
			Description: table.At(cells, "description"),
		}
		// A skipped row's placeholder is not a description: reading it back as
		// one would make an archived repository look described, and a repository
		// that came back out of the archive would never earn a real sentence.
		if strings.HasPrefix(entry.Description, "(") {
			entry.Description = ""
		}
		entry.Files = readPhrases(filepath.Join(dir, FilesDir, name+".tsv"))
		state[name] = entry
	}
	return state, nil
}

// Table is a parsed catalog: its rows keyed by repository name, and the header
// they are to be read against. A tree written by a different version of this
// tool has a different header, which is the whole reason the header travels with
// the rows.
type Table struct {
	Header []string
	Rows   map[string][]string
}

// At reads one cell by column name. A column this tree does not have reads
// empty, which is what an older tree should look like - absent, never
// misaligned.
func (t Table) At(cells []string, column string) string {
	for i, name := range t.Header {
		if name == column {
			return cell(cells, i)
		}
	}
	return ""
}

// readCatalog parses the catalog into a Table, tolerating a missing file.
func readCatalog(path string) (Table, error) {
	table := Table{Header: catalogHeader, Rows: map[string][]string{}}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return table, nil
	}
	if err != nil {
		return Table{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLine)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, tab)
		if cells[0] == "" {
			continue
		}
		if cells[0] == catalogHeader[0] {
			// The tree's own header wins over this build's: it is what its rows
			// were written against.
			table.Header = cells
			continue
		}
		table.Rows[cells[0]] = cells
	}
	return table, scanner.Err()
}

// readPhrases reads the per-file phrases a previous build wrote, so a
// repository whose fingerprint still matches keeps them instead of buying them
// again. A missing or unreadable file yields none, which costs a redescribe and
// never a wrong answer.
func readPhrases(path string) map[string]string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	phrases := map[string]string{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLine)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, tab)
		if len(cells) < 4 || cells[0] == filesHeader[0] {
			continue
		}
		if cells[3] != "" {
			phrases[cells[0]] = cells[3]
		}
	}
	return phrases
}

// maxLine bounds one TSV line. A description is a sentence and a path is a
// path; a line far past this is a corrupted file, not a long row.
const maxLine = 1 << 20

func cell(cells []string, i int) string {
	if i < len(cells) {
		return cells[i]
	}
	return ""
}

func write(path, content string) error {
	return os.WriteFile(path, []byte(content), fileMode)
}
