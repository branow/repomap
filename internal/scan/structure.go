package scan

import (
	"sort"
	"strconv"
	"strings"

	"github.com/branow/repomap/internal/catalog"
)

// MaxSubdirs is how many immediate subdirectories one top-level directory
// contributes to the structure summary. A repository with three hundred
// packages under one directory says everything it has to say in the first
// handful of them, and the rest is prompt budget spent on a list.
const MaxSubdirs = 8

// Structure renders a two-level directory sketch: top-level directories with a
// few of their immediate subdirectories, then the top-level files. A flat list
// of two thousand paths is a poor way to say what a repository IS; the shape of
// its top two levels is what a person reads first too.
func Structure(files []catalog.File) string {
	subdirs := map[string]map[string]struct{}{}
	var roots []string

	for _, file := range files {
		slash := strings.Index(file.Path, "/")
		if slash < 0 {
			roots = append(roots, file.Path)
			continue
		}
		dir, rest := file.Path[:slash], file.Path[slash+1:]
		if _, ok := subdirs[dir]; !ok {
			subdirs[dir] = map[string]struct{}{}
		}
		if second := strings.Index(rest, "/"); second > 0 {
			subdirs[dir][rest[:second]] = struct{}{}
		}
	}

	dirs := make([]string, 0, len(subdirs))
	for dir := range subdirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	sort.Strings(roots)

	var out strings.Builder
	for _, dir := range dirs {
		out.WriteString(dir + "/\n")
		children := make([]string, 0, len(subdirs[dir]))
		for child := range subdirs[dir] {
			children = append(children, child)
		}
		sort.Strings(children)
		for i, child := range children {
			if i == MaxSubdirs {
				out.WriteString("  ... " +
					strconv.Itoa(len(children)-MaxSubdirs) + " more\n")
				break
			}
			out.WriteString("  " + child + "/\n")
		}
	}
	for _, file := range roots {
		out.WriteString(file + "\n")
	}
	if out.Len() == 0 {
		return "(no files)"
	}
	return strings.TrimRight(out.String(), "\n")
}
