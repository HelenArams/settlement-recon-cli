package match

import (
	"strings"
	"testing"

	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

func TestTier2MatchesWhenAuthCodeIsMissing(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 12000, "EUR", "9Z8Y7X", "M-GROCER", at(10, 18))}
	clearings := []model.Clearing{clearing("C-1", "tok", 12000, "EUR", "", "M-GROCER", day(12), model.Presentment)}
	r := run(t, auths, clearings, DefaultOptions())[0]
	if r.Outcome != Matched || r.Tier != 2 || r.Delta != 0 || r.Note != "" {
		t.Errorf("got %+v, want clean tier-2 match", r)
	}
}

func TestTier2ToleranceBoundary(t *testing.T) {
	opt := DefaultOptions() // 50 bps
	tests := []struct {
		name    string
		cleared int64
		outcome Outcome
		tier    int
	}{
		{"exactly on tolerance", 10050, Matched, 2},
		{"one minor unit past tolerance", 10051, Partial, 3},
		{"below by exactly tolerance", 9950, Matched, 2},
		{"below past tolerance", 9949, Partial, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auths := []model.Auth{auth("A-1", "tok", 10000, "EUR", "ABC123", "M-FX", at(13, 14))}
			clearings := []model.Clearing{clearing("C-1", "tok", tc.cleared, "EUR", "ABC123", "M-FX", day(14), model.Presentment)}
			r := run(t, auths, clearings, opt)[0]
			if r.Outcome != tc.outcome || r.Tier != tc.tier {
				t.Errorf("cleared %d: got %s tier %d, want %s tier %d (%s)", tc.cleared, r.Outcome, r.Tier, tc.outcome, tc.tier, r.Note)
			}
		})
	}
}

func TestZeroToleranceStillMatchesExactAmountAtTier2(t *testing.T) {
	opt := Options{ToleranceBps: 0, WindowDays: 3}
	auths := []model.Auth{auth("A-1", "tok", 10000, "EUR", "", "M", at(13, 14))}
	clearings := []model.Clearing{
		clearing("C-same", "tok", 10000, "EUR", "", "M", day(14), model.Presentment),
	}
	r := run(t, auths, clearings, opt)[0]
	if r.Outcome != Matched || r.Tier != 2 {
		t.Errorf("got %+v", r)
	}
}

func TestTier2FXDriftNote(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 8999, "USD", "4D5E6F", "M-SHOP", at(11, 12))}
	clearings := []model.Clearing{clearing("C-1", "tok", 9020, "USD", "4D5E6F", "M-SHOP", day(12), model.Presentment)}
	r := run(t, auths, clearings, DefaultOptions())[0]
	if r.Outcome != Matched || r.Tier != 2 || r.Delta != 21 {
		t.Fatalf("got %+v", r)
	}
	if r.Note != "amount off by +0.21 (23 bps)" {
		t.Errorf("note = %q", r.Note)
	}
}

func TestTier3PartialCaptureNote(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 25000, "USD", "7G8H9I", "M-HOTEL", at(9, 15))}
	clearings := []model.Clearing{clearing("C-1", "tok", 18000, "USD", "7G8H9I", "M-HOTEL", day(12), model.Presentment)}
	r := run(t, auths, clearings, DefaultOptions())[0]
	if r.Outcome != Partial || r.Tier != 3 || r.Delta != -7000 || !r.Outcome.IsException() {
		t.Fatalf("got %+v", r)
	}
	if r.Note != "amount off by -70.00 (2800 bps), outside 50 bps tolerance" {
		t.Errorf("note = %q", r.Note)
	}
}

func TestWindowBoundary(t *testing.T) {
	opt := DefaultOptions() // 3 days
	tests := []struct {
		name    string
		settled int
		outcome Outcome
	}{
		{"same day", 9, Matched},
		{"last day of window", 12, Matched},
		{"one day past window", 13, UnmatchedClearing},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auths := []model.Auth{auth("A-1", "tok", 5000, "EUR", "", "M", at(9, 23))}
			clearings := []model.Clearing{clearing("C-1", "tok", 5000, "EUR", "", "M", day(tc.settled), model.Presentment)}
			got := byRef(run(t, auths, clearings, opt))
			if got["C-1"].Outcome != tc.outcome {
				t.Errorf("settled day %d: got %s, want %s", tc.settled, got["C-1"].Outcome, tc.outcome)
			}
		})
	}
}

func TestWindowIsSymmetric(t *testing.T) {
	// A settlement dated the day before the auth (time zone skew) stays inside the window.
	auths := []model.Auth{auth("A-1", "tok", 5000, "EUR", "", "M", at(10, 0))}
	clearings := []model.Clearing{clearing("C-1", "tok", 5000, "EUR", "", "M", day(9), model.Presentment)}
	r := run(t, auths, clearings, DefaultOptions())[0]
	if r.Outcome != Matched || !strings.Contains(r.Note, "before the authorization") {
		t.Errorf("got %+v", r)
	}
}

func TestTier1LatePresentmentIsMatchedButNoted(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 4800, "USD", "3J4K5L", "M-CAR", at(3, 16))}
	clearings := []model.Clearing{clearing("C-1", "tok", 4800, "USD", "3J4K5L", "M-CAR", day(13), model.Presentment)}
	r := run(t, auths, clearings, DefaultOptions())[0]
	if r.Outcome != Matched || r.Tier != 1 {
		t.Fatalf("got %+v", r)
	}
	if r.Note != "settled 10 days after auth, outside 3-day window" {
		t.Errorf("note = %q", r.Note)
	}
}

func TestTier1MerchantRemapIsNoted(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 999, "GBP", "6M7N8O", "M-APP-STORE", at(12, 11))}
	clearings := []model.Clearing{clearing("C-1", "tok", 999, "GBP", "6M7N8O", "M-APPSTORE", day(13), model.Presentment)}
	r := run(t, auths, clearings, DefaultOptions())[0]
	if r.Note != "merchant differs: M-APP-STORE vs M-APPSTORE" {
		t.Errorf("note = %q", r.Note)
	}
}

func TestTier2RequiresSameMerchant(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 5000, "EUR", "", "M-A", at(10, 10))}
	clearings := []model.Clearing{clearing("C-1", "tok", 5000, "EUR", "", "M-B", day(11), model.Presentment)}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["C-1"].Outcome != UnmatchedClearing {
		t.Errorf("got %+v, want unmatched: merchant differs and there is no auth code", got["C-1"])
	}
}

func TestTier2PrefersClosestAmount(t *testing.T) {
	auths := []model.Auth{
		auth("A-far", "tok", 10040, "EUR", "", "M", at(10, 10)),
		auth("A-near", "tok", 10010, "EUR", "", "M", at(10, 11)),
	}
	clearings := []model.Clearing{clearing("C-1", "tok", 10000, "EUR", "", "M", day(11), model.Presentment)}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["C-1"].Auth.ID != "A-near" {
		t.Errorf("matched %s, want A-near", got["C-1"].Auth.ID)
	}
}

func TestTier3PrefersClosestDate(t *testing.T) {
	auths := []model.Auth{
		auth("A-older", "tok", 10000, "EUR", "", "M", at(9, 10)),
		auth("A-newer", "tok", 30000, "EUR", "", "M", at(11, 10)),
	}
	clearings := []model.Clearing{clearing("C-1", "tok", 20000, "EUR", "", "M", day(12), model.Presentment)}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["C-1"].Outcome != Partial || got["C-1"].Auth.ID != "A-newer" {
		t.Errorf("got %+v, want partial against A-newer", got["C-1"])
	}
}

func TestAuthCodeMismatchOnTier2IsNoted(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 5000, "EUR", "AAA111", "M", at(10, 10))}
	clearings := []model.Clearing{clearing("C-1", "tok", 5000, "EUR", "BBB222", "M", day(11), model.Presentment)}
	r := run(t, auths, clearings, DefaultOptions())[0]
	if r.Outcome != Matched || r.Tier != 2 || r.Note != "auth code differs: AAA111 vs BBB222" {
		t.Errorf("got %+v", r)
	}
}

func TestOptionsValidate(t *testing.T) {
	bad := []Options{
		{ToleranceBps: -1, WindowDays: 3},
		{ToleranceBps: 10001, WindowDays: 3},
		{ToleranceBps: 50, WindowDays: -1},
	}
	for _, opt := range bad {
		if _, err := Reconcile(nil, nil, opt); err == nil {
			t.Errorf("%+v accepted", opt)
		}
	}
	if err := (Options{ToleranceBps: 10000, WindowDays: 0}).Validate(); err != nil {
		t.Errorf("edge values rejected: %v", err)
	}
}
