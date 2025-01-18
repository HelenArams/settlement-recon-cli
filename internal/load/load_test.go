package load

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

const authsCSV = `id,card_token,amount,currency,auth_code,merchant_id,timestamp
A-1,tok_1,42.10,EUR,1a2b3c,M-1,2025-02-10T08:15:00Z
A-2,tok_2,1500,JPY,,M-2,2025-02-10 18:42:00
A-3,tok_3,0.99,gbp,9Z8Y7X,M-3,2025-02-11
`

func TestAuthsHappyPath(t *testing.T) {
	got, err := Auths(strings.NewReader(authsCSV), "auths.csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d auths, want 3", len(got))
	}
	want0 := model.Auth{
		ID: "A-1", CardToken: "tok_1", Amount: 4210, Currency: "EUR", AuthCode: "1A2B3C",
		MerchantID: "M-1", Timestamp: time.Date(2025, 2, 10, 8, 15, 0, 0, time.UTC),
	}
	if got[0] != want0 {
		t.Errorf("row 1 = %+v, want %+v", got[0], want0)
	}
	if got[1].Amount != 1500 || got[1].AuthCode != "" {
		t.Errorf("row 2 = %+v, want JPY 1500 without auth code", got[1])
	}
	if !got[1].Timestamp.Equal(time.Date(2025, 2, 10, 18, 42, 0, 0, time.UTC)) {
		t.Errorf("row 2 timestamp = %v, want 2025-02-10 18:42 UTC", got[1].Timestamp)
	}
	if got[2].Currency != "GBP" || got[2].Amount != 99 {
		t.Errorf("row 3 = %+v, want GBP 99", got[2])
	}
}

func TestAuthsHeaderAliases(t *testing.T) {
	in := `Auth ID,Token,Amt,CCY,Approval-Code,MID,Created At,ignored
A-1,tok_1,10.00,EUR,ABC123,M-1,2025-02-10T08:15:00Z,whatever
`
	got, err := Auths(strings.NewReader(in), "aliases.csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "A-1" || got[0].AuthCode != "ABC123" || got[0].MerchantID != "M-1" {
		t.Errorf("got %+v", got)
	}
}

func TestAuthsMissingRequiredColumn(t *testing.T) {
	in := "id,card_token,amount,currency,timestamp\nA-1,tok,1.00,EUR,2025-01-01\n"
	_, err := Auths(strings.NewReader(in), "auths.csv")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"auths.csv", "missing required column(s) merchant_id", "header has: id, card_token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	}
	var errs Errors
	if errors.As(err, &errs) {
		t.Error("a header problem must not be reported as row errors")
	}
}

func TestAuthsDuplicateColumn(t *testing.T) {
	in := "id,card_token,amount,currency,merchant_id,mid,timestamp\n"
	_, err := Auths(strings.NewReader(in), "auths.csv")
	if err == nil || !strings.Contains(err.Error(), `columns 5 and 6 both map to "merchant_id"`) {
		t.Fatalf("got %v", err)
	}
}

func TestAuthsEmptyFile(t *testing.T) {
	_, err := Auths(strings.NewReader(""), "auths.csv")
	if err == nil || !strings.Contains(err.Error(), "auths.csv: file is empty") {
		t.Fatalf("got %v", err)
	}
}

func TestAuthsHeaderOnly(t *testing.T) {
	got, err := Auths(strings.NewReader("id,card_token,amount,currency,merchant_id,timestamp\n"), "auths.csv")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want no rows and no error", got, err)
	}
}

func TestAuthsRowErrorsAreCollectedWithLineNumbers(t *testing.T) {
	in := `id,card_token,amount,currency,auth_code,merchant_id,timestamp
A-1,tok_1,42.10,EUR,1A2B3C,M-1,2025-02-10T08:15:00Z
A-2,tok_2,12.345,EUR,,M-2,2025-02-10T08:15:00Z
A-3,tok_3,10.00,EUR,,M-3,yesterday
A-4,tok_4,10.00,EUROS,,M-4,2025-02-10T08:15:00Z
A-5,tok_5,-1.00,EUR,,M-5,2025-02-10T08:15:00Z
,tok_6,1.00,EUR,,,2025-02-10T08:15:00Z
A-7,tok_7,1.00,EUR,,M-7
A-8,tok_8,1.00,EUR,,M-8,2025-02-10T08:15:00Z
`
	got, err := Auths(strings.NewReader(in), "auths.csv")
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("want Errors, got %T: %v", err, err)
	}
	if len(got) != 2 {
		t.Errorf("good rows = %d, want 2 (A-1 and A-8)", len(got))
	}
	wantLines := []string{
		`auths.csv:3: amount: amount "12.345" has more than 2 decimals for EUR`,
		`auths.csv:4: timestamp: cannot parse timestamp "yesterday"`,
		`auths.csv:5: currency: currency "EUROS" is not a 3-letter ISO code`,
		`auths.csv:6: amount is negative`,
		`auths.csv:7: id is empty`,
		`auths.csv:8: wrong number of fields`,
	}
	if len(errs) != len(wantLines) {
		t.Fatalf("got %d errors, want %d:\n%v", len(errs), len(wantLines), err)
	}
	for i, want := range wantLines {
		if !strings.Contains(errs[i].Error(), want) {
			t.Errorf("error %d = %q, want containing %q", i, errs[i], want)
		}
	}
	if strings.Contains(errs[4].Error(), "merchant_id is empty") == false {
		t.Errorf("row 7 should report both empty id and empty merchant_id: %q", errs[4])
	}
}

func TestAuthsStopAfterTooManyErrors(t *testing.T) {
	var b strings.Builder
	b.WriteString("id,card_token,amount,currency,merchant_id,timestamp\n")
	for i := 0; i < maxErrors+20; i++ {
		b.WriteString("A,tok,bad,EUR,M,2025-01-01\n")
	}
	_, err := Auths(strings.NewReader(b.String()), "auths.csv")
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("want Errors, got %v", err)
	}
	if len(errs) != maxErrors {
		t.Errorf("collected %d errors, want cap of %d", len(errs), maxErrors)
	}
	if !strings.Contains(err.Error(), "stopped after 50 errors") {
		t.Errorf("message should say it stopped: %q", err)
	}
}

const clearingCSV = `arn,card_token,amount,currency,auth_code,merchant_id,settlement_date,type
74000000000000000000001,tok_1,42.10,EUR,1A2B3C,M-1,2025-02-11,presentment
74000000000000000000002,tok_1,42.10,EUR,,M-1,20250212,Reversal
74000000000000000000003,tok_2,199.00,eur,1T2U3V,M-2,2025-02-13T00:00:00Z,first_chargeback
`

func TestClearingsHappyPath(t *testing.T) {
	got, err := Clearings(strings.NewReader(clearingCSV), "clearing.csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}
	want0 := model.Clearing{
		ARN: "74000000000000000000001", CardToken: "tok_1", Amount: 4210, Currency: "EUR",
		AuthCode: "1A2B3C", MerchantID: "M-1",
		SettlementDate: time.Date(2025, 2, 11, 0, 0, 0, 0, time.UTC), Type: model.Presentment,
	}
	if got[0] != want0 {
		t.Errorf("row 1 = %+v, want %+v", got[0], want0)
	}
	if got[1].Type != model.Reversal || !got[1].SettlementDate.Equal(time.Date(2025, 2, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("row 2 = %+v, want reversal on 2025-02-12", got[1])
	}
	if got[2].Type != model.Chargeback || got[2].Currency != "EUR" {
		t.Errorf("row 3 = %+v, want EUR chargeback", got[2])
	}
}

func TestClearingsTypeDefaultsToPresentment(t *testing.T) {
	in := "arn,card_token,amount,currency,merchant_id,settlement_date\n1,tok,1.00,EUR,M,2025-02-11\n"
	got, err := Clearings(strings.NewReader(in), "c.csv")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
	if got[0].Type != model.Presentment {
		t.Errorf("type = %q, want presentment", got[0].Type)
	}
}

func TestClearingsRowErrors(t *testing.T) {
	in := `arn,card_token,amount,currency,merchant_id,settlement_date,type
1,tok,1.00,EUR,M,2025-02-11,refund
2,tok,1.00,EUR,M,11/02/2025,presentment
,tok,1.00,EUR,M,2025-02-11,presentment
`
	_, err := Clearings(strings.NewReader(in), "c.csv")
	var errs Errors
	if !errors.As(err, &errs) || len(errs) != 3 {
		t.Fatalf("got %v", err)
	}
	wantLines := []string{
		`c.csv:2: type: unknown clearing type "refund"`,
		`c.csv:3: settlement_date: cannot parse date "11/02/2025"`,
		`c.csv:4: arn is empty`,
	}
	for i, want := range wantLines {
		if !strings.Contains(errs[i].Error(), want) {
			t.Errorf("error %d = %q, want containing %q", i, errs[i], want)
		}
	}
}

func TestFilesNotFound(t *testing.T) {
	if _, err := AuthsFile("testdata/does-not-exist.csv"); err == nil {
		t.Error("AuthsFile: expected error")
	}
	if _, err := ClearingsFile("testdata/does-not-exist.csv"); err == nil {
		t.Error("ClearingsFile: expected error")
	}
}

func TestParseTimestampZones(t *testing.T) {
	got, err := parseTimestamp("2025-02-10T23:30:00+01:00")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2025, 2, 10, 22, 30, 0, 0, time.UTC)
	if !got.Equal(want) || got.Location() != time.UTC {
		t.Errorf("got %v, want %v in UTC", got, want)
	}
	if _, err := parseTimestamp(""); err == nil {
		t.Error("empty timestamp accepted")
	}
}

func TestRowErrorFormatting(t *testing.T) {
	e := &RowError{File: "f.csv", Line: 7, Field: "amount", Msg: "bad"}
	if got := e.Error(); got != "f.csv:7: amount: bad" {
		t.Errorf("got %q", got)
	}
	e = &RowError{File: "f.csv", Msg: "bad"}
	if got := e.Error(); got != "f.csv: bad" {
		t.Errorf("got %q", got)
	}
}
