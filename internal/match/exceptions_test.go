package match

import (
	"testing"

	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

func TestDuplicateARN(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 1200, "EUR", "4W5X6Y", "M-COFFEE", at(12, 9))}
	dupARN := "74000000000000000001009"
	clearings := []model.Clearing{
		clearing(dupARN, "tok", 1200, "EUR", "4W5X6Y", "M-COFFEE", day(13), model.Presentment),
		clearing(dupARN, "tok", 1200, "EUR", "4W5X6Y", "M-COFFEE", day(13), model.Presentment),
	}
	results := run(t, auths, clearings, DefaultOptions())
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2: %+v", len(results), results)
	}
	// Same date and ARN: the match is reported before the duplicate.
	if results[0].Outcome != Matched || results[0].Tier != 1 {
		t.Errorf("first = %+v, want matched", results[0])
	}
	if results[1].Outcome != Duplicate || results[1].Auth != nil || results[1].Note == "" || !results[1].Outcome.IsException() {
		t.Errorf("second = %+v, want duplicate without auth", results[1])
	}
}

func TestDuplicateDoesNotConsumeASecondAuthorization(t *testing.T) {
	// Two identical purchases, each approved. The second clearing record is a
	// resend of the first (same ARN), not the capture of the second auth.
	auths := []model.Auth{
		auth("A-1", "tok", 1200, "EUR", "CODE01", "M", at(12, 9)),
		auth("A-2", "tok", 1200, "EUR", "CODE02", "M", at(12, 10)),
	}
	clearings := []model.Clearing{
		clearing("ARN-1", "tok", 1200, "EUR", "CODE01", "M", day(13), model.Presentment),
		clearing("ARN-1", "tok", 1200, "EUR", "CODE01", "M", day(13), model.Presentment),
	}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["A-2"].Outcome != OpenAuth {
		t.Errorf("A-2 = %+v, want still open", got["A-2"])
	}
}

func TestDuplicateDetectionIsByARNOnly(t *testing.T) {
	// Different amounts under one ARN is still a duplicate: the ARN is the
	// scheme's unique record reference, so the second row is a resend.
	auths := []model.Auth{auth("A-1", "tok", 1200, "EUR", "CODE01", "M", at(12, 9))}
	clearings := []model.Clearing{
		clearing("ARN-1", "tok", 1200, "EUR", "CODE01", "M", day(13), model.Presentment),
		clearing("ARN-1", "tok", 1300, "EUR", "CODE01", "M", day(14), model.Presentment),
	}
	got := run(t, auths, clearings, DefaultOptions())
	if got[0].Outcome != Matched || got[1].Outcome != Duplicate || got[1].Clearing.Amount != 1300 {
		t.Errorf("got %+v", got)
	}
}

func TestExpiryBoundary(t *testing.T) {
	opt := DefaultOptions() // 7 days
	asOf := day(14)
	tests := []struct {
		name    string
		authDay int
		outcome Outcome
	}{
		{"authorized today", 14, OpenAuth},
		{"exactly at expiry", 7, OpenAuth},
		{"one day past expiry", 6, ExpiredAuth},
		{"long gone", 1, ExpiredAuth},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auths := []model.Auth{auth("A-1", "tok", 1550, "GBP", "2K3L4M", "M-TRANSIT", at(tc.authDay, 7))}
			opt.AsOf = asOf
			r := run(t, auths, nil, opt)[0]
			if r.Outcome != tc.outcome {
				t.Errorf("auth day %d as of %s: got %s (%s), want %s", tc.authDay, asOf.Format("01-02"), r.Outcome, r.Note, tc.outcome)
			}
		})
	}
}

func TestExpiredAuthNoteAndException(t *testing.T) {
	opt := DefaultOptions()
	opt.AsOf = day(14)
	auths := []model.Auth{auth("A-1", "tok", 1550, "GBP", "2K3L4M", "M-TRANSIT", at(5, 7))}
	r := run(t, auths, nil, opt)[0]
	if r.Outcome != ExpiredAuth || !r.Outcome.IsException() {
		t.Fatalf("got %+v", r)
	}
	if r.Note != "open for 9 days, expiry is 7" {
		t.Errorf("note = %q", r.Note)
	}
}

func TestExpiryUsesDerivedAsOf(t *testing.T) {
	// No explicit as-of: the latest settlement date in the file (02-14) is used.
	auths := []model.Auth{
		auth("A-old", "tok_a", 1550, "GBP", "AAAAAA", "M", at(5, 7)),
		auth("A-new", "tok_a", 6000, "GBP", "BBBBBB", "M", at(13, 19)),
	}
	clearings := []model.Clearing{clearing("C-1", "tok_z", 100, "EUR", "", "M", day(14), model.Presentment)}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["A-old"].Outcome != ExpiredAuth {
		t.Errorf("A-old = %+v, want expired", got["A-old"])
	}
	if got["A-new"].Outcome != OpenAuth {
		t.Errorf("A-new = %+v, want open", got["A-new"])
	}
}

func TestLinkedAuthIsNeverExpired(t *testing.T) {
	opt := DefaultOptions()
	opt.AsOf = day(28)
	auths := []model.Auth{auth("A-1", "tok", 19900, "EUR", "1T2U3V", "M", at(1, 10))}
	clearings := []model.Clearing{clearing("C-cb", "tok", 19900, "EUR", "1T2U3V", "M", day(27), model.Chargeback)}
	results := run(t, auths, clearings, opt)
	for _, r := range results {
		if r.Outcome == ExpiredAuth || r.Outcome == OpenAuth {
			t.Errorf("auth referenced by a chargeback reported as %s", r.Outcome)
		}
	}
}

func TestNegativeExpiryRejected(t *testing.T) {
	opt := DefaultOptions()
	opt.ExpireDays = -1
	if _, err := Reconcile(nil, nil, opt); err == nil {
		t.Error("negative expiry accepted")
	}
}

func TestOutcomeRankCoversEveryOutcome(t *testing.T) {
	seen := map[int]bool{}
	for _, o := range []Outcome{Matched, Partial, UnmatchedClearing, ExpiredAuth, Duplicate, OpenAuth} {
		r := outcomeRank(o)
		if r >= len(Outcomes) || seen[r] {
			t.Errorf("%s has rank %d", o, r)
		}
		seen[r] = true
	}
	if outcomeRank("BOGUS") != len(Outcomes) {
		t.Error("unknown outcome should sort last")
	}
}
