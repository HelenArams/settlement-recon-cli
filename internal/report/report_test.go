package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/HelenArams/settlement-recon-cli/internal/match"
	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

func day(d int) time.Time { return time.Date(2025, 2, d, 0, 0, 0, 0, time.UTC) }

// fixture builds a reconciliation with every outcome and two currencies.
func fixture(t *testing.T) *match.Reconciliation {
	t.Helper()
	auths := []model.Auth{
		{ID: "A-1", CardToken: "tok_a", Amount: 4210, Currency: "EUR", AuthCode: "1A2B3C", MerchantID: "M-1", Timestamp: day(10).Add(8 * time.Hour)},
		{ID: "A-2", CardToken: "tok_a", Amount: 25000, Currency: "EUR", AuthCode: "7G8H9I", MerchantID: "M-HOTEL", Timestamp: day(9).Add(15 * time.Hour)},
		{ID: "A-3", CardToken: "tok_b", Amount: 1550, Currency: "GBP", AuthCode: "2K3L4M", MerchantID: "M-3", Timestamp: day(1).Add(7 * time.Hour)},
		{ID: "A-4", CardToken: "tok_b", Amount: 6000, Currency: "GBP", AuthCode: "5N6O7P", MerchantID: "M-4", Timestamp: day(13).Add(19 * time.Hour)},
		{ID: "A-5", CardToken: "tok_a", Amount: 19900, Currency: "EUR", AuthCode: "1T2U3V", MerchantID: "M-5", Timestamp: day(2).Add(10 * time.Hour)},
	}
	clearings := []model.Clearing{
		{ARN: "ARN-1", CardToken: "tok_a", Amount: 4210, Currency: "EUR", AuthCode: "1A2B3C", MerchantID: "M-1", SettlementDate: day(11), Type: model.Presentment},
		{ARN: "ARN-1", CardToken: "tok_a", Amount: 4210, Currency: "EUR", AuthCode: "1A2B3C", MerchantID: "M-1", SettlementDate: day(11), Type: model.Presentment},
		{ARN: "ARN-2", CardToken: "tok_a", Amount: 18000, Currency: "EUR", AuthCode: "7G8H9I", MerchantID: "M-HOTEL", SettlementDate: day(12), Type: model.Presentment},
		{ARN: "ARN-3", CardToken: "tok_z", Amount: 7325, Currency: "EUR", MerchantID: "M-9", SettlementDate: day(13), Type: model.Presentment},
		{ARN: "ARN-4", CardToken: "tok_a", Amount: 19900, Currency: "EUR", AuthCode: "1T2U3V", MerchantID: "M-5", SettlementDate: day(3), Type: model.Presentment},
		{ARN: "ARN-5", CardToken: "tok_a", Amount: 19900, Currency: "EUR", AuthCode: "1T2U3V", MerchantID: "M-5", SettlementDate: day(14), Type: model.Chargeback},
		{ARN: "ARN-6", CardToken: "tok_a", Amount: 4210, Currency: "EUR", AuthCode: "1A2B3C", MerchantID: "M-1", SettlementDate: day(13), Type: model.Reversal},
	}
	rec, err := match.Reconcile(auths, clearings, match.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestSummarize(t *testing.T) {
	s := Summarize(fixture(t))
	if s.Total != 9 {
		t.Errorf("Total = %d, want 9", s.Total)
	}
	wantOutcomes := map[match.Outcome]int{
		match.Matched:           4, // ARN-1, ARN-4, ARN-5 (chargeback), ARN-6 (reversal)
		match.Partial:           1, // ARN-2 hotel
		match.UnmatchedClearing: 1, // ARN-3
		match.ExpiredAuth:       1, // A-3
		match.Duplicate:         1, // second ARN-1
		match.OpenAuth:          1, // A-4
	}
	for o, want := range wantOutcomes {
		if got := s.ByOutcome[o]; got != want {
			t.Errorf("ByOutcome[%s] = %d, want %d", o, got, want)
		}
	}
	if s.Exceptions != 4 {
		t.Errorf("Exceptions = %d, want 4 (partial, unmatched, expired, duplicate)", s.Exceptions)
	}
	if s.ByTier[3] != 1 || s.ByTier[1] != 4 || s.ByTier[2] != 0 {
		t.Errorf("ByTier = %v", s.ByTier)
	}

	if len(s.Net) != 1 || s.Net[0].Currency != "EUR" {
		t.Fatalf("Net = %+v, want EUR only (GBP never cleared)", s.Net)
	}
	eur := s.Net[0]
	// Presented: 42.10 + 180.00 + 73.25 + 199.00 (duplicate ARN-1 excluded).
	if eur.Presented != 4210+18000+7325+19900 {
		t.Errorf("Presented = %d", eur.Presented)
	}
	if eur.Reversed != -4210 || eur.ChargedBack != -19900 {
		t.Errorf("Reversed = %d, ChargedBack = %d", eur.Reversed, eur.ChargedBack)
	}
	if eur.Net != eur.Presented+eur.Reversed+eur.ChargedBack {
		t.Errorf("Net = %d does not add up", eur.Net)
	}
}

func TestParseFormat(t *testing.T) {
	for in, want := range map[string]Format{"table": Table, "JSON": JSON, "Csv": CSV} {
		got, err := ParseFormat(in)
		if err != nil || got != want {
			t.Errorf("ParseFormat(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseFormat("xml"); err == nil {
		t.Error("xml accepted")
	}
	if err := Write(&bytes.Buffer{}, fixture(t), Format("xml")); err == nil {
		t.Error("Write accepted unknown format")
	}
}

func TestWriteTable(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, fixture(t), Table); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Settlement reconciliation as of 2025-02-14",
		"tolerance 50 bps, window 3 days, auth expiry 7 days",
		"TOTAL                   9  exceptions: 4",
		"NET SETTLEMENT",
		"DATE        OUTCOME",
		"PARTIAL             T3    A-2",
		"DUPLICATE           -     -        ARN-1",
		"EXPIRED_AUTH        -     A-3      -",
		"-199.00",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table output lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("trailing whitespace in %q", line)
		}
	}
}

func TestWriteTableEmpty(t *testing.T) {
	rec, _ := match.Reconcile(nil, nil, match.DefaultOptions())
	var buf bytes.Buffer
	if err := WriteTable(&buf, rec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "TOTAL                   0  exceptions: 0") {
		t.Errorf("unexpected output:\n%s", buf.String())
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, fixture(t), JSON); err != nil {
		t.Fatal(err)
	}
	var got struct {
		AsOf    string `json:"as_of"`
		Options struct {
			ToleranceBps int `json:"tolerance_bps"`
		} `json:"options"`
		Summary struct {
			Total         int            `json:"total"`
			Exceptions    int            `json:"exceptions"`
			Outcomes      map[string]int `json:"outcomes"`
			Tiers         map[string]int `json:"tiers"`
			NetSettlement []struct {
				Currency string `json:"currency"`
				Net      string `json:"net"`
			} `json:"net_settlement"`
		} `json:"summary"`
		Results []struct {
			Outcome  string          `json:"outcome"`
			Tier     int             `json:"tier"`
			Auth     json.RawMessage `json:"auth"`
			Clearing json.RawMessage `json:"clearing"`
			Delta    string          `json:"delta"`
		} `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if got.AsOf != "2025-02-14" || got.Options.ToleranceBps != 50 {
		t.Errorf("header = %+v", got)
	}
	if got.Summary.Total != 9 || got.Summary.Exceptions != 4 || got.Summary.Outcomes["PARTIAL"] != 1 || got.Summary.Tiers["3"] != 1 {
		t.Errorf("summary = %+v", got.Summary)
	}
	if len(got.Summary.NetSettlement) != 1 || got.Summary.NetSettlement[0].Net != "253.25" {
		t.Errorf("net = %+v", got.Summary.NetSettlement)
	}
	if len(got.Results) != 9 {
		t.Fatalf("results = %d", len(got.Results))
	}
	var sawNullAuth, sawDelta bool
	for _, r := range got.Results {
		if string(r.Auth) == "null" {
			sawNullAuth = true
		}
		if r.Delta != "" {
			sawDelta = true
		}
	}
	if !sawNullAuth || !sawDelta {
		t.Errorf("expected null auth on unmatched rows and deltas on matched rows")
	}
}

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, fixture(t), CSV); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatalf("invalid CSV: %v", err)
	}
	if len(rows) != 10 {
		t.Fatalf("got %d rows, want header + 9", len(rows))
	}
	if strings.Join(rows[0], ",") != strings.Join(csvHeader, ",") {
		t.Errorf("header = %v", rows[0])
	}
	col := func(name string) int {
		for i, h := range csvHeader {
			if h == name {
				return i
			}
		}
		t.Fatalf("no column %s", name)
		return -1
	}
	var sawReversal bool
	for _, row := range rows[1:] {
		if len(row) != len(csvHeader) {
			t.Errorf("row has %d fields: %v", len(row), row)
		}
		if row[col("clearing_type")] == "reversal" {
			sawReversal = true
			if row[col("cleared")] != "-42.10" {
				t.Errorf("reversal cleared = %q, want signed -42.10", row[col("cleared")])
			}
		}
		if row[col("outcome")] == "EXPIRED_AUTH" && (row[col("arn")] != "" || row[col("auth_id")] != "A-3") {
			t.Errorf("expired row = %v", row)
		}
	}
	if !sawReversal {
		t.Error("no reversal row")
	}
}

func TestTableAlignment(t *testing.T) {
	tb := newTable("NAME", "AMOUNT", "NOTE").alignRight(1)
	tb.add("a", "1.00", "x")
	tb.add("longer", "12345.00", "")
	var buf bytes.Buffer
	if err := tb.render(&buf); err != nil {
		t.Fatal(err)
	}
	want := "NAME      AMOUNT  NOTE\na           1.00  x\nlonger  12345.00\n"
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}
