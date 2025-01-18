package load

import (
	"fmt"
	"strings"
)

// maxErrors caps how many row errors a single load collects before it gives
// up. A file with thousands of bad rows is a format problem, not a data one,
// and a screenful of context is more useful than a flood.
const maxErrors = 50

// RowError describes one problem in one record of a CSV file.
type RowError struct {
	File  string
	Line  int    // 1-based line in the file; 0 when unknown
	Field string // canonical column name; empty for whole-row problems
	Msg   string
}

func (e *RowError) Error() string {
	var b strings.Builder
	b.WriteString(e.File)
	if e.Line > 0 {
		fmt.Fprintf(&b, ":%d", e.Line)
	}
	b.WriteString(": ")
	if e.Field != "" {
		b.WriteString(e.Field)
		b.WriteString(": ")
	}
	b.WriteString(e.Msg)
	return b.String()
}

// Errors is every row error found in a file, in file order.
type Errors []*RowError

func (e Errors) Error() string {
	lines := make([]string, 0, len(e)+1)
	for _, re := range e {
		lines = append(lines, re.Error())
	}
	if len(e) >= maxErrors {
		lines = append(lines, fmt.Sprintf("%s: stopped after %d errors", e[0].File, maxErrors))
	}
	return strings.Join(lines, "\n")
}
