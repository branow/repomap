// Package render writes the index tree. Everything it writes is plain text
// that a reader can open, grep and diff without this tool, a parser or a
// schema.
package render

import (
	"math"
	"strconv"
	"strings"
)

const tab = "\t"

const (
	dirMode  = 0o755
	fileMode = 0o644
)

// clean makes a value safe to put in a cell. A model-written sentence can
// contain a tab, a newline or a carriage return, any of which would end the
// cell or the row early and corrupt every row after it.
//
// Runs collapse to a single space rather than one space each: a CRLF is one
// line break, and rendering it as two spaces leaves a gap in the middle of a
// sentence the reader has to wonder about.
func clean(value string) string { return strings.Join(strings.Fields(value), " ") }

// TSV renders a header and its rows. Only the rows are cleaned.
func TSV(header []string, rows [][]string) string {
	var out strings.Builder
	out.WriteString(strings.Join(header, tab))
	for _, cells := range rows {
		out.WriteString("\n")
		for i, cell := range cells {
			if i > 0 {
				out.WriteString(tab)
			}
			out.WriteString(clean(cell))
		}
	}
	out.WriteString("\n")
	return out.String()
}

// Size renders a byte count in the unit a human would use.
func Size(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return strconv.FormatInt(int64(math.Round(float64(bytes)/(1<<30))), 10) + " GB"
	case bytes >= 1<<20:
		return strconv.FormatInt(int64(math.Round(float64(bytes)/(1<<20))), 10) + " MB"
	case bytes >= 1<<10:
		return strconv.FormatInt(int64(math.Round(float64(bytes)/(1<<10))), 10) + " KB"
	default:
		return strconv.FormatInt(bytes, 10) + " B"
	}
}
