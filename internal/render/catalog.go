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
var catalogHeader = []string{
	"repo", "files", "size", "commit", "activity", "fingerprint", "description",
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

	rows, err := readCatalog(path)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		rows[entry.Key()] = row(entry)
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

func row(entry catalog.Entry) []string {
	description := entry.Description
	if entry.Skip != "" {
		// A skipped repository says so in the column a reader looks at, rather
		// than leaving a blank that is indistinguishable from a failed describe.
		description = "(" + entry.Skip + ")"
	}
	return []string{
		entry.Key(),
		strconv.Itoa(len(entry.Tree.Files)),
		Size(entry.Tree.Bytes),
		entry.Tree.LastCommit,
		entry.Repo.Activity.String(),
		entry.Fingerprint,
		description,
	}
}

// State reads back what the previous build recorded, from the files it wrote.
// A tree that has never been built returns an empty state and no error: a first
// build is not a failure.
func State(dir string) (map[string]catalog.State, error) {
	rows, err := readCatalog(filepath.Join(dir, CatalogFile))
	if err != nil {
		return nil, err
	}
	state := make(map[string]catalog.State, len(rows))
	for name, cells := range rows {
		entry := catalog.State{
			Activity:    catalog.Signal(cell(cells, 4)),
			Fingerprint: cell(cells, 5),
			Description: cell(cells, 6),
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

// readCatalog parses the catalog into rows keyed by repository name, tolerating
// a missing file and ignoring the header.
func readCatalog(path string) (map[string][]string, error) {
	rows := map[string][]string{}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return rows, nil
	}
	if err != nil {
		return nil, err
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
		if cells[0] == "" || cells[0] == catalogHeader[0] {
			continue
		}
		rows[cells[0]] = cells
	}
	return rows, scanner.Err()
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
