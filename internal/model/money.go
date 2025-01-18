package model

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// exponents lists the currencies whose minor unit is not 1/100. Everything
// else is assumed to have two decimals, which covers the ISO 4217 majority.
var exponents = map[string]int{
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0,
	"KRW": 0, "PYG": 0, "RWF": 0, "UGX": 0, "VND": 0, "VUV": 0, "XAF": 0,
	"XOF": 0, "XPF": 0,
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,
}

// Exponent returns the number of decimal places in a currency's minor unit.
func Exponent(currency string) int {
	if e, ok := exponents[strings.ToUpper(currency)]; ok {
		return e
	}
	return 2
}

// ParseAmount converts a decimal string such as "12.34" into minor units of
// the given currency. It accepts an optional sign and fewer fractional digits
// than the currency allows ("12.5" EUR is 1250), but rejects more, so a
// three-decimal amount in a two-decimal currency is an error rather than a
// silent rounding.
func ParseAmount(s, currency string) (int64, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return 0, fmt.Errorf("amount is empty")
	}
	neg := false
	switch raw[0] {
	case '-':
		neg = true
		raw = raw[1:]
	case '+':
		raw = raw[1:]
	}
	whole, frac, _ := strings.Cut(raw, ".")
	exp := Exponent(currency)
	if whole == "" && frac == "" {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	if whole == "" {
		whole = "0"
	}
	if len(frac) > exp {
		return 0, fmt.Errorf("amount %q has more than %d decimals for %s", s, exp, strings.ToUpper(currency))
	}
	if !digitsOnly(whole) || !digitsOnly(frac) {
		return 0, fmt.Errorf("invalid amount %q", s)
	}
	frac += strings.Repeat("0", exp-len(frac))

	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q is out of range", s)
	}
	f := int64(0)
	if frac != "" {
		f, _ = strconv.ParseInt(frac, 10, 64) // digits-only and short: cannot fail
	}
	scale := pow10(exp)
	if w > (math.MaxInt64-f)/scale {
		return 0, fmt.Errorf("amount %q is out of range", s)
	}
	minor := w*scale + f
	if neg {
		minor = -minor
	}
	return minor, nil
}

// FormatAmount renders minor units as a decimal string with the currency's
// exponent, e.g. 1250 EUR -> "12.50", 1500 JPY -> "1500", -5 BHD -> "-0.005".
func FormatAmount(minor int64, currency string) string {
	exp := Exponent(currency)
	neg := minor < 0
	// Work on the magnitude as uint64 so MinInt64 does not overflow.
	mag := uint64(minor)
	if neg {
		mag = uint64(-minor)
	}
	digits := strconv.FormatUint(mag, 10)
	if exp == 0 {
		if neg {
			return "-" + digits
		}
		return digits
	}
	if len(digits) <= exp {
		digits = strings.Repeat("0", exp-len(digits)+1) + digits
	}
	cut := len(digits) - exp
	out := digits[:cut] + "." + digits[cut:]
	if neg {
		return "-" + out
	}
	return out
}

// WithinBps reports whether got deviates from want by at most bps basis
// points of want (1 bp = 0.01%). The comparison is exact integer arithmetic,
// so a deviation of exactly bps is inside the tolerance. A zero reference
// amount only tolerates a zero got.
func WithinBps(want, got int64, bps int) bool {
	if want == 0 {
		return got == 0
	}
	diff := abs(got - want)
	return diff*10_000 <= abs(want)*int64(bps)
}

// Bps returns the deviation of got from want in whole basis points, rounded
// towards zero. It is informational; use WithinBps for decisions.
func Bps(want, got int64) int64 {
	if want == 0 {
		return 0
	}
	return abs(got-want) * 10_000 / abs(want)
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func pow10(n int) int64 {
	p := int64(1)
	for i := 0; i < n; i++ {
		p *= 10
	}
	return p
}

func digitsOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
