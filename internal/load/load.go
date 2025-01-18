// Package load reads authorization and clearing records from CSV files.
//
// Exports from issuer processors and card schemes rarely agree on column
// names or value formats, so headers are matched case-insensitively against
// a set of aliases, amounts are parsed as exact decimals in the currency's
// minor unit, and several timestamp layouts are accepted. Every problem is
// reported with the file name and 1-based line number so it can be fixed at
// the source.
package load

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

// AuthsFile loads authorizations from a CSV file on disk.
func AuthsFile(path string) ([]model.Auth, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Auths(f, path)
}

// ClearingsFile loads clearing records from a CSV file on disk.
func ClearingsFile(path string) ([]model.Clearing, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Clearings(f, path)
}

// Auths parses authorizations from r. name is used in error messages only.
// When rows are malformed the returned error is an Errors value listing every
// problem found (up to a cap), and the slice holds the rows that did parse.
func Auths(r io.Reader, name string) ([]model.Auth, error) {
	var out []model.Auth
	err := readRecords(r, name, authColumns, func(line int, rec []string, l layout) *RowError {
		a, rerr := parseAuth(name, line, rec, l)
		if rerr != nil {
			return rerr
		}
		out = append(out, a)
		return nil
	})
	return out, err
}

// Clearings parses clearing records from r. See Auths for error semantics.
func Clearings(r io.Reader, name string) ([]model.Clearing, error) {
	var out []model.Clearing
	err := readRecords(r, name, clearingColumns, func(line int, rec []string, l layout) *RowError {
		c, rerr := parseClearing(name, line, rec, l)
		if rerr != nil {
			return rerr
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

// readRecords drives encoding/csv, resolves the header and hands each data
// row to fn. Row-level problems are collected; structural problems (bad
// header, unreadable input) abort immediately.
func readRecords(r io.Reader, name string, cols []column, fn func(int, []string, layout) *RowError) error {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.ReuseRecord = true

	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: file is empty", name)
	}
	if err != nil {
		return rowErrorFromCSV(name, err)
	}
	l, err := resolveLayout(name, header, cols)
	if err != nil {
		return err
	}

	var errs Errors
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line, _ := cr.FieldPos(0)
		if err != nil {
			errs = append(errs, rowErrorFromCSV(name, err))
		} else if rerr := fn(line, rec, l); rerr != nil {
			errs = append(errs, rerr)
		}
		if len(errs) >= maxErrors {
			break
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func rowErrorFromCSV(name string, err error) *RowError {
	var pe *csv.ParseError
	if errors.As(err, &pe) {
		msg := pe.Err.Error()
		if errors.Is(pe.Err, csv.ErrFieldCount) {
			msg = "wrong number of fields"
		}
		return &RowError{File: name, Line: pe.Line, Msg: msg}
	}
	return &RowError{File: name, Msg: err.Error()}
}

// field returns the trimmed value of a canonical column, or "" when the
// column is absent from the file.
func field(rec []string, l layout, name string) string {
	i, ok := l[name]
	if !ok || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

func parseAuth(file string, line int, rec []string, l layout) (model.Auth, *RowError) {
	fail := func(col, msg string) (model.Auth, *RowError) {
		return model.Auth{}, &RowError{File: file, Line: line, Field: col, Msg: msg}
	}
	a := model.Auth{
		ID:         field(rec, l, "id"),
		CardToken:  field(rec, l, "card_token"),
		Currency:   strings.ToUpper(field(rec, l, "currency")),
		AuthCode:   strings.ToUpper(field(rec, l, "auth_code")),
		MerchantID: field(rec, l, "merchant_id"),
	}
	if err := model.ValidateCurrency(a.Currency); err != nil {
		return fail("currency", err.Error())
	}
	amount, err := model.ParseAmount(field(rec, l, "amount"), a.Currency)
	if err != nil {
		return fail("amount", err.Error())
	}
	a.Amount = amount
	ts, err := parseTimestamp(field(rec, l, "timestamp"))
	if err != nil {
		return fail("timestamp", err.Error())
	}
	a.Timestamp = ts
	if err := a.Validate(); err != nil {
		return fail("", err.Error())
	}
	return a, nil
}

func parseClearing(file string, line int, rec []string, l layout) (model.Clearing, *RowError) {
	fail := func(col, msg string) (model.Clearing, *RowError) {
		return model.Clearing{}, &RowError{File: file, Line: line, Field: col, Msg: msg}
	}
	c := model.Clearing{
		ARN:        field(rec, l, "arn"),
		CardToken:  field(rec, l, "card_token"),
		Currency:   strings.ToUpper(field(rec, l, "currency")),
		AuthCode:   strings.ToUpper(field(rec, l, "auth_code")),
		MerchantID: field(rec, l, "merchant_id"),
		Type:       model.Presentment,
	}
	if err := model.ValidateCurrency(c.Currency); err != nil {
		return fail("currency", err.Error())
	}
	amount, err := model.ParseAmount(field(rec, l, "amount"), c.Currency)
	if err != nil {
		return fail("amount", err.Error())
	}
	c.Amount = amount
	d, err := parseDate(field(rec, l, "settlement_date"))
	if err != nil {
		return fail("settlement_date", err.Error())
	}
	c.SettlementDate = d
	if l.has("type") {
		t, err := model.ParseClearingType(field(rec, l, "type"))
		if err != nil {
			return fail("type", err.Error())
		}
		c.Type = t
	}
	if err := c.Validate(); err != nil {
		return fail("", err.Error())
	}
	return c, nil
}

// timestampLayouts are tried in order. Layouts without a zone are read as UTC.
var timestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// dateLayouts cover the compact YYYYMMDD many scheme files use as well as
// ISO dates; full timestamps are accepted and truncated to the day.
var dateLayouts = []string{
	"2006-01-02",
	"20060102",
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
}

func parseTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("timestamp is empty")
	}
	for _, layout := range timestampLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse timestamp %q (want RFC 3339 or YYYY-MM-DD HH:MM:SS)", s)
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("date is empty")
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			y, m, d := t.UTC().Date()
			return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q (want YYYY-MM-DD or YYYYMMDD)", s)
}
