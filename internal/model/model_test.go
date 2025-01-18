package model

import (
	"strings"
	"testing"
	"time"
)

func TestParseClearingType(t *testing.T) {
	ok := map[string]ClearingType{
		"presentment":          Presentment,
		"PRESENTMENT":          Presentment,
		" First_Presentment ":  Presentment,
		"sale":                 Presentment,
		"reversal":             Reversal,
		"presentment_reversal": Reversal,
		"chargeback":           Chargeback,
		"first_chargeback":     Chargeback,
		"dispute":              Chargeback,
	}
	for in, want := range ok {
		got, err := ParseClearingType(in)
		if err != nil {
			t.Errorf("ParseClearingType(%q) unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseClearingType(%q) = %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "refund", "auth", "presentments"} {
		if _, err := ParseClearingType(in); err == nil {
			t.Errorf("ParseClearingType(%q) expected error", in)
		}
	}
}

func TestClearingSignedAmount(t *testing.T) {
	tests := []struct {
		typ  ClearingType
		want int64
	}{
		{Presentment, 4210},
		{Reversal, -4210},
		{Chargeback, -4210},
	}
	for _, tc := range tests {
		c := Clearing{Amount: 4210, Type: tc.typ}
		if got := c.SignedAmount(); got != tc.want {
			t.Errorf("%s SignedAmount = %d, want %d", tc.typ, got, tc.want)
		}
	}
}

func TestAuthDate(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	a := Auth{Timestamp: time.Date(2025, 2, 11, 0, 30, 0, 0, loc)} // 23:30 UTC the day before
	want := time.Date(2025, 2, 10, 0, 0, 0, 0, time.UTC)
	if got := a.Date(); !got.Equal(want) {
		t.Errorf("Date() = %v, want %v", got, want)
	}
}

func TestAuthValidate(t *testing.T) {
	good := Auth{
		ID: "A-1", CardToken: "tok", Amount: 100, Currency: "EUR",
		AuthCode: "1A2B3C", MerchantID: "M-1", Timestamp: time.Now(),
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid auth rejected: %v", err)
	}
	noCode := good
	noCode.AuthCode = ""
	if err := noCode.Validate(); err != nil {
		t.Errorf("auth without auth_code should be valid, got %v", err)
	}

	bad := Auth{Amount: -1, Currency: "eur"}
	err := bad.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"id is empty", "card_token is empty", "amount is negative", `currency "eur"`, "merchant_id is empty", "timestamp is missing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestClearingValidate(t *testing.T) {
	good := Clearing{
		ARN: "7401", CardToken: "tok", Amount: 100, Currency: "EUR",
		MerchantID: "M-1", SettlementDate: time.Now(), Type: Presentment,
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid clearing rejected: %v", err)
	}
	bad := Clearing{Currency: "EU", Type: "refund"}
	err := bad.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"arn is empty", `currency "EU"`, "settlement_date is missing", `unknown clearing type "refund"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestValidateCurrency(t *testing.T) {
	for _, ccy := range []string{"EUR", "USD", "JPY"} {
		if err := ValidateCurrency(ccy); err != nil {
			t.Errorf("%s rejected: %v", ccy, err)
		}
	}
	for _, ccy := range []string{"", "EU", "EURO", "eur", "E1R", "€UR"} {
		if err := ValidateCurrency(ccy); err == nil {
			t.Errorf("%q accepted", ccy)
		}
	}
}
