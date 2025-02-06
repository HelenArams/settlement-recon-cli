package report

import (
	"io"
	"strings"
	"unicode/utf8"
)

// table lays out rows in aligned columns separated by two spaces. Text
// columns are left-aligned, numeric ones right-aligned, and trailing blanks
// are trimmed so the output diffs cleanly.
type table struct {
	cols  []string
	right []bool
	rows  [][]string
}

func newTable(cols ...string) *table {
	return &table{cols: cols, right: make([]bool, len(cols))}
}

func (t *table) alignRight(idx ...int) *table {
	for _, i := range idx {
		t.right[i] = true
	}
	return t
}

func (t *table) add(cells ...string) {
	row := make([]string, len(t.cols))
	copy(row, cells)
	t.rows = append(t.rows, row)
}

func (t *table) render(w io.Writer) error {
	widths := make([]int, len(t.cols))
	for i, c := range t.cols {
		widths[i] = utf8.RuneCountInString(c)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	writeRow := func(cells []string) {
		for i, cell := range cells {
			pad := widths[i] - utf8.RuneCountInString(cell)
			if t.right[i] {
				b.WriteString(strings.Repeat(" ", pad))
				b.WriteString(cell)
			} else {
				b.WriteString(cell)
				if i < len(cells)-1 {
					b.WriteString(strings.Repeat(" ", pad))
				}
			}
			if i < len(cells)-1 {
				b.WriteString("  ")
			}
		}
		line := strings.TrimRight(b.String(), " ")
		b.Reset()
		b.WriteString(line)
		b.WriteByte('\n')
		_, _ = io.WriteString(w, b.String())
		b.Reset()
	}
	writeRow(t.cols)
	for _, row := range t.rows {
		writeRow(row)
	}
	return nil
}
