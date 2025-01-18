package model

import (
	"math"
	"strings"
	"testing"
)

func TestParseAmount(t *testing.T) {
	tests := []struct {
		in, ccy string
		want    int64
		wantErr string
	}{
		{"12.34", "EUR", 1234, ""},
		{"12", "EUR", 1200, ""},
		{"12.5", "EUR", 1250, ""},
		{"0.05", "EUR", 5, ""},
		{".5", "EUR", 50, ""},
		{"-3.21", "GBP", -321, ""},
		{"+3.21", "GBP", 321, ""},
		{" 42.10 ", "EUR", 4210, ""},
		{"1500", "JPY", 1500, ""},
		{"1.234", "BHD", 1234, ""},
		{"1.2", "BHD", 1200, ""},
		{"0", "USD", 0, ""},
		{"", "EUR", 0, "empty"},
		{"   ", "EUR", 0, "empty"},
		{"-", "EUR", 0, "invalid"},
		{".", "EUR", 0, "invalid"},
		{"12.345", "EUR", 0, "more than 2 decimals"},
		{"15.0", "JPY", 0, "more than 0 decimals"},
		{"1,000.00", "EUR", 0, "invalid"},
		{"12.3x", "EUR", 0, "invalid"},
		{"1e3", "EUR", 0, "invalid"},
		{"99999999999999999999", "EUR", 0, "out of range"},
		{"92233720368547758.08", "EUR", 0, "out of range"},
		{"92233720368547758.07", "EUR", math.MaxInt64, ""},
	}
	for _, tc := range tests {
		got, err := ParseAmount(tc.in, tc.ccy)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ParseAmount(%q, %s) error = %v, want containing %q", tc.in, tc.ccy, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseAmount(%q, %s) unexpected error: %v", tc.in, tc.ccy, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseAmount(%q, %s) = %d, want %d", tc.in, tc.ccy, got, tc.want)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		minor int64
		ccy   string
		want  string
	}{
		{1234, "EUR", "12.34"},
		{5, "EUR", "0.05"},
		{0, "EUR", "0.00"},
		{-5, "EUR", "-0.05"},
		{-123456, "USD", "-1234.56"},
		{1500, "JPY", "1500"},
		{-1500, "JPY", "-1500"},
		{1234, "BHD", "1.234"},
		{5, "KWD", "0.005"},
		{math.MaxInt64, "EUR", "92233720368547758.07"},
		{math.MinInt64, "EUR", "-92233720368547758.08"},
	}
	for _, tc := range tests {
		if got := FormatAmount(tc.minor, tc.ccy); got != tc.want {
			t.Errorf("FormatAmount(%d, %s) = %q, want %q", tc.minor, tc.ccy, got, tc.want)
		}
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	for _, ccy := range []string{"EUR", "JPY", "BHD"} {
		for _, minor := range []int64{0, 1, 99, 100, 101, -1, -100, 123456789} {
			s := FormatAmount(minor, ccy)
			back, err := ParseAmount(s, ccy)
			if err != nil {
				t.Fatalf("%s %d -> %q -> error %v", ccy, minor, s, err)
			}
			if back != minor {
				t.Errorf("%s %d -> %q -> %d", ccy, minor, s, back)
			}
		}
	}
}

func TestExponent(t *testing.T) {
	if got := Exponent("eur"); got != 2 {
		t.Errorf("Exponent(eur) = %d, want 2", got)
	}
	if got := Exponent("JPY"); got != 0 {
		t.Errorf("Exponent(JPY) = %d, want 0", got)
	}
	if got := Exponent("KWD"); got != 3 {
		t.Errorf("Exponent(KWD) = %d, want 3", got)
	}
	if got := Exponent("XXX"); got != 2 {
		t.Errorf("Exponent(XXX) = %d, want default 2", got)
	}
}

func TestWithinBps(t *testing.T) {
	tests := []struct {
		want, got int64
		bps       int
		ok        bool
	}{
		{10000, 10000, 0, true},   // exact match passes a zero tolerance
		{10000, 10001, 0, false},  // one minor unit off fails a zero tolerance
		{10000, 10050, 50, true},  // exactly 50 bps is inside
		{10000, 10051, 50, false}, // 51 bps is outside
		{10000, 9950, 50, true},   // tolerance is symmetric
		{10000, 9949, 50, false},
		{8999, 9020, 50, true}, // 23 bps FX drift
		{0, 0, 50, true},
		{0, 1, 50, false},
		{-10000, -10050, 50, true}, // sign of the reference does not matter
		{1, 1, 10000, true},
		{1, 2, 10000, true}, // 100% tolerance accepts doubling
		{1, 3, 10000, false},
	}
	for _, tc := range tests {
		if got := WithinBps(tc.want, tc.got, tc.bps); got != tc.ok {
			t.Errorf("WithinBps(%d, %d, %d) = %v, want %v", tc.want, tc.got, tc.bps, got, tc.ok)
		}
	}
}

func TestBps(t *testing.T) {
	tests := []struct {
		want, got, bps int64
	}{
		{10000, 10050, 50},
		{10000, 9950, 50},
		{8999, 9020, 23},
		{25000, 18000, 2800},
		{0, 500, 0},
		{10000, 10000, 0},
	}
	for _, tc := range tests {
		if got := Bps(tc.want, tc.got); got != tc.bps {
			t.Errorf("Bps(%d, %d) = %d, want %d", tc.want, tc.got, got, tc.bps)
		}
	}
}
