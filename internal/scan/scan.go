// Package scan reads one working tree into the tree shape the rest of the
// build carries. It reads tracked files only, counts lines and bytes, and
// reads nothing else: a scan that opened source files to look for structure
// would be a parser per language, and the model already reads paths.
package scan

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/branow/repomap/internal/catalog"
	"github.com/branow/repomap/internal/git"
)

// MaxLineCountBytes caps how much of one file is read to count its lines. Past
// it the count is reported as what was counted so far: a generated bundle or a
// checked-in dump is not worth minutes of reading, and the exact line count of
// one is not a fact anybody needs.
const MaxLineCountBytes = 4 << 20

// Scan reads the working tree at dir. It returns an EmptyError for a repository
// with no commits, which is a normal state for a freshly created repository and
// never an error worth failing a build over.
func Scan(ctx context.Context, runner git.Runner, dir string) (catalog.Tree, error) {
	head, err := git.Head(ctx, runner, dir)
	if err != nil || head == "" {
		return catalog.Tree{}, &EmptyError{Dir: dir}
	}

	paths, err := git.Files(ctx, runner, dir)
	if err != nil {
		return catalog.Tree{}, err
	}
	branch, _ := git.Branch(ctx, runner, dir)
	lastCommit, _ := git.LastCommit(ctx, runner, dir)

	tree := catalog.Tree{
		Head:       head,
		Branch:     branch,
		LastCommit: lastCommit,
		Readme:     readme(dir),
		Files:      make([]catalog.File, 0, len(paths)),
	}
	for _, path := range paths {
		file := catalog.File{Path: path}
		if info, err := os.Stat(filepath.Join(dir, path)); err == nil {
			file.Bytes = info.Size()
			tree.Bytes += info.Size()
			file.Lines = countLines(filepath.Join(dir, path))
		}
		tree.Files = append(tree.Files, file)
	}
	// Sorted by path so the fingerprint, and the file the reader opens, do not
	// depend on the order git happened to list them in.
	sort.Slice(tree.Files, func(i, j int) bool { return tree.Files[i].Path < tree.Files[j].Path })
	return tree, nil
}

// readmeNames are tried in order. Only the root is looked at: a README two
// directories down describes that directory, not this repository.
var readmeNames = []string{"README.md", "README.rst", "README.txt", "README", "readme.md"}

func readme(dir string) string {
	for _, name := range readmeNames {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if len(body) > catalog.MaxReadme {
			body = body[:catalog.MaxReadme]
		}
		return strings.TrimSpace(string(body))
	}
	return ""
}

// countLines counts newlines, stopping at the cap. A file with no trailing
// newline still counts its last line, because a reader comparing this number
// with their editor's would otherwise find it off by one on most short files.
func countLines(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()

	reader := bufio.NewReaderSize(io.LimitReader(file, MaxLineCountBytes), 64<<10)
	buffer := make([]byte, 64<<10)
	lines, lastByte := 0, byte('\n')
	for {
		n, err := reader.Read(buffer)
		for i := 0; i < n; i++ {
			if buffer[i] == '\n' {
				lines++
			}
		}
		if n > 0 {
			lastByte = buffer[n-1]
		}
		if err != nil {
			break
		}
	}
	if lastByte != '\n' {
		lines++
	}
	return lines
}

// EmptyError reports a working tree with no commits: nothing to index, and not
// a failure.
type EmptyError struct{ Dir string }

func (e *EmptyError) Error() string { return "no commits in " + e.Dir }
