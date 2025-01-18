package load

import (
	"fmt"
	"sort"
	"strings"
)

// column is one logical field of a record and the header spellings that map
// onto it. Processor and scheme exports never agree on names, so matching is
// by alias rather than position.
type column struct {
	name     string
	aliases  []string
	required bool
}

var authColumns = []column{
	{"id", []string{"id", "auth_id", "authorization_id", "txn_id", "transaction_id"}, true},
	{"card_token", []string{"card_token", "token", "pan_token", "card_id"}, true},
	{"amount", []string{"amount", "auth_amount", "amt"}, true},
	{"currency", []string{"currency", "ccy", "currency_code"}, true},
	{"auth_code", []string{"auth_code", "approval_code", "authorization_code"}, false},
	{"merchant_id", []string{"merchant_id", "mid", "card_acceptor_id"}, true},
	{"timestamp", []string{"timestamp", "auth_time", "auth_timestamp", "created_at", "datetime"}, true},
}

var clearingColumns = []column{
	{"arn", []string{"arn", "acquirer_reference_number", "acquirer_ref"}, true},
	{"card_token", []string{"card_token", "token", "pan_token", "card_id"}, true},
	{"amount", []string{"amount", "clearing_amount", "settlement_amount", "amt"}, true},
	{"currency", []string{"currency", "ccy", "currency_code"}, true},
	{"auth_code", []string{"auth_code", "approval_code", "authorization_code"}, false},
	{"merchant_id", []string{"merchant_id", "mid", "card_acceptor_id"}, true},
	{"settlement_date", []string{"settlement_date", "settle_date", "clearing_date", "processing_date"}, true},
	{"type", []string{"type", "record_type", "transaction_type", "message_type"}, false},
}

// layout is the resolved position of each canonical column in a file.
type layout map[string]int

// has reports whether an optional column is present.
func (l layout) has(name string) bool {
	_, ok := l[name]
	return ok
}

// normalizeHeader folds a header cell to the form aliases are written in:
// lower case, surrounding whitespace removed, spaces and dashes as underscores.
func normalizeHeader(cell string) string {
	s := strings.ToLower(strings.TrimSpace(cell))
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return s
}

// resolveLayout maps a header row onto the canonical columns. It fails when a
// required column is missing or when two header cells resolve to the same
// column, because silently picking one would hide a real export problem.
func resolveLayout(file string, header []string, cols []column) (layout, error) {
	byAlias := make(map[string]string, len(cols)*4)
	for _, c := range cols {
		for _, a := range c.aliases {
			byAlias[a] = c.name
		}
	}

	l := make(layout, len(cols))
	for i, cell := range header {
		name, ok := byAlias[normalizeHeader(cell)]
		if !ok {
			continue // unknown columns are allowed and ignored
		}
		if prev, dup := l[name]; dup {
			return nil, fmt.Errorf("%s: columns %d and %d both map to %q", file, prev+1, i+1, name)
		}
		l[name] = i
	}

	var missing []string
	for _, c := range cols {
		if c.required && !l.has(c.name) {
			missing = append(missing, c.name)
		}
	}
	if len(missing) > 0 {
		have := make([]string, len(header))
		for i, h := range header {
			have[i] = strings.TrimSpace(h)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("%s: missing required column(s) %s; header has: %s",
			file, strings.Join(missing, ", "), strings.Join(have, ", "))
	}
	return l, nil
}
