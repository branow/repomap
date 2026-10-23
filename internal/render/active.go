package render

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/branow/repomap/internal/catalog"
)

// DefaultActiveYears is the window a repository counts as active in. A year
// rather than a quarter: a service that is finished and correct gets a
// dependency bump twice a year and is still exactly the service a reader is
// looking for, while something genuinely abandoned has not been touched in
// several.
const DefaultActiveYears = 1

// Active rewrites the active view from the catalog. It reads the file the build
// just wrote rather than the entries it wrote it from, so the view cannot
// disagree with the catalog even after a narrowed run that touched a handful of
// rows.
func Active(dir string, years int, now time.Time) (kept, total int, err error) {
	if years <= 0 {
		years = DefaultActiveYears
	}
	table, err := readCatalog(filepath.Join(dir, CatalogFile))
	if err != nil {
		return 0, 0, err
	}

	cutoff := now.AddDate(-years, 0, 0).Format(dateLayout)
	names := make([]string, 0, len(table.Rows))
	for name := range table.Rows {
		names = append(names, name)
	}
	sort.Strings(names)

	ordered := make([][]string, 0, len(names))
	for _, name := range names {
		// A skipped row is certainly dead whatever its date says. It is never
		// scanned, so its commit date is usually missing, and an undated row
		// would otherwise be kept - an archived repository looking active.
		if catalog.IsPlaceholder(table.At(table.Rows[name], "description")) {
			continue
		}
		// A row whose commit date is missing or unparseable is kept. The view
		// exists to hide what is certainly dead, and a row that cannot be dated
		// is not certainly anything.
		if date := table.At(table.Rows[name], "commit"); isDate(date) && date < cutoff {
			continue
		}
		ordered = append(ordered, table.Rows[name])
	}

	preamble := strings.Join([]string{
		"# repositories with a commit since " + cutoff,
		"# " + strconv.Itoa(len(names)-len(ordered)) +
			" older or skipped repositories omitted - " + CatalogFile + " has the full list",
		"# generated " + now.UTC().Format(time.RFC3339) + "; do not edit, a build rewrites it",
	}, "\n") + "\n"

	path := filepath.Join(dir, ActiveFile)
	return len(ordered), len(names), write(path, preamble+TSV(catalogHeader, ordered))
}

const dateLayout = "2006-01-02"

func isDate(value string) bool {
	_, err := time.Parse(dateLayout, value)
	return err == nil
}
