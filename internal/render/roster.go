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
	"time"
)

// RosterFile is the tree's own table of contents, written at the root rather
// than inside one workspace's directory: one row per workspace the tree holds.
//
// It exists because the reader is not the builder. Whoever reads the index has
// no repomap installed, no forge credential and no way to ask what was built -
// so the roster, and the guide beside it, are what let a tree explain itself to
// a reader who arrives with nothing but the files.
const RosterFile = "workspaces.tsv"

// GuideFile is the reader's copy of the layout, written beside the roster for
// the same reason: an agent that has never heard of this tool must be able to
// use the tree from the tree alone.
const GuideFile = "README.md"

// Roster is one workspace's row: which forge it came from, what it holds, and
// when it was built.
type Roster struct {
	Workspace string
	Forge     string
	Slug      string
	Repos     int
	Active    int
	Files     int
	Built     time.Time
}

var rosterHeader = []string{"workspace", "forge", "account", "repos", "active", "files", "built"}

// WriteRoster merges one workspace's row into the roster, leaving every other
// row exactly as it was. A tree holds several workspaces built at different
// times by different people, so a build that rewrote the whole file would erase
// the stamps of the workspaces it did not touch.
func WriteRoster(root string, entry Roster) error {
	if err := os.MkdirAll(root, dirMode); err != nil {
		return err
	}
	path := filepath.Join(root, RosterFile)

	table, err := readRoster(path)
	if err != nil {
		return err
	}
	rows := table.Rows
	rows[entry.Workspace] = []string{
		entry.Workspace,
		entry.Forge,
		entry.Slug,
		strconv.Itoa(entry.Repos),
		strconv.Itoa(entry.Active),
		strconv.Itoa(entry.Files),
		entry.Built.UTC().Format(time.RFC3339),
	}

	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([][]string, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, rows[name])
	}
	return write(path, TSV(rosterHeader, ordered))
}

// ReadRoster reports every workspace a tree holds, newest build first. It is
// how a reader - and the plugin that finds trees on this machine - asks what is
// in one without running a build.
func ReadRoster(root string) ([]Roster, error) {
	table, err := readRoster(filepath.Join(root, RosterFile))
	if err != nil {
		return nil, err
	}
	out := make([]Roster, 0, len(table.Rows))
	for _, cells := range table.Rows {
		built, _ := time.Parse(time.RFC3339, table.At(cells, "built"))
		out = append(out, Roster{
			Workspace: table.At(cells, "workspace"),
			Forge:     table.At(cells, "forge"),
			Slug:      table.At(cells, "account"),
			Repos:     number(table.At(cells, "repos")),
			Active:    number(table.At(cells, "active")),
			Files:     number(table.At(cells, "files")),
			Built:     built,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Workspace < out[j].Workspace })
	return out, nil
}

func readRoster(path string) (Table, error) {
	table := Table{Header: rosterHeader, Rows: map[string][]string{}}
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
		if cells[0] == rosterHeader[0] {
			table.Header = cells
			continue
		}
		table.Rows[cells[0]] = cells
	}
	return table, scanner.Err()
}

func number(value string) int {
	n, _ := strconv.Atoi(value)
	return n
}
