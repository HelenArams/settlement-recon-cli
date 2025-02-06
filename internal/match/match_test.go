package match

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

func day(d int) time.Time { return time.Date(2025, 2, d, 0, 0, 0, 0, time.UTC) }

func at(d, hour int) time.Time { return time.Date(2025, 2, d, hour, 0, 0, 0, time.UTC) }

func auth(id, token string, amount int64, ccy, code, merchant string, ts time.Time) model.Auth {
	return model.Auth{ID: id, CardToken: token, Amount: amount, Currency: ccy, AuthCode: code, MerchantID: merchant, Timestamp: ts}
}

func clearing(arn, token string, amount int64, ccy, code, merchant string, date time.Time, typ model.ClearingType) model.Clearing {
	return model.Clearing{ARN: arn, CardToken: token, Amount: amount, Currency: ccy, AuthCode: code, MerchantID: merchant, SettlementDate: date, Type: typ}
}

func run(t *testing.T, auths []model.Auth, clearings []model.Clearing, opt Options) []Result {
	t.Helper()
	rec, err := Reconcile(auths, clearings, opt)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	return rec.Results
}

// byRef indexes results by ARN or auth ID so tests can assert without
// depending on report order.
func byRef(results []Result) map[string]Result {
	out := make(map[string]Result, len(results))
	for _, r := range results {
		out[r.Ref()] = r
	}
	return out
}

func TestTier1ExactMatch(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 4210, "EUR", "1A2B3C", "M-1", at(10, 8))}
	clearings := []model.Clearing{clearing("C-1", "tok", 4210, "EUR", "1A2B3C", "M-1", day(11), model.Presentment)}
	results := run(t, auths, clearings, DefaultOptions())
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	r := results[0]
	if r.Outcome != Matched || r.Tier != 1 || r.Delta != 0 || r.Note != "" {
		t.Errorf("got %+v, want tier-1 match without note", r)
	}
	if r.Auth.ID != "A-1" || r.Clearing.ARN != "C-1" {
		t.Errorf("linked %s to %s", r.Clearing.ARN, r.Auth.ID)
	}
}

func TestTier1IgnoresMerchantID(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 999, "GBP", "6M7N8O", "M-APP-STORE", at(12, 11))}
	clearings := []model.Clearing{clearing("C-1", "tok", 999, "GBP", "6M7N8O", "M-APPSTORE", day(13), model.Presentment)}
	r := run(t, auths, clearings, DefaultOptions())[0]
	if r.Outcome != Matched || r.Tier != 1 {
		t.Errorf("got %+v, want tier-1 match despite merchant remap", r)
	}
}

func TestTier1RequiresSameCurrency(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 4210, "EUR", "1A2B3C", "M-1", at(10, 8))}
	clearings := []model.Clearing{clearing("C-1", "tok", 4210, "USD", "1A2B3C", "M-1", day(11), model.Presentment)}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["C-1"].Outcome != UnmatchedClearing || got["A-1"].Outcome != OpenAuth {
		t.Errorf("got %+v", got)
	}
}

func TestTier1SkippedWithoutAuthCode(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 4210, "EUR", "1A2B3C", "M-1", at(10, 8))}
	clearings := []model.Clearing{clearing("C-1", "tok", 4210, "EUR", "", "M-1", day(11), model.Presentment)}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["C-1"].Outcome == Matched && got["C-1"].Tier == 1 {
		t.Errorf("a clearing without an auth code must not match at tier 1: %+v", got["C-1"])
	}
}

func TestTier1PrefersNearestAuthorization(t *testing.T) {
	auths := []model.Auth{
		auth("A-old", "tok", 500, "EUR", "ABC123", "M-1", at(6, 9)),
		auth("A-new", "tok", 500, "EUR", "ABC123", "M-1", at(10, 9)),
	}
	clearings := []model.Clearing{clearing("C-1", "tok", 500, "EUR", "ABC123", "M-1", day(11), model.Presentment)}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["C-1"].Auth.ID != "A-new" {
		t.Errorf("matched %s, want the authorization closest to settlement", got["C-1"].Auth.ID)
	}
	if got["A-old"].Outcome != OpenAuth {
		t.Errorf("A-old = %+v, want open", got["A-old"])
	}
}

func TestPresentmentConsumesAuthorization(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 4210, "EUR", "1A2B3C", "M-1", at(10, 8))}
	clearings := []model.Clearing{
		clearing("C-1", "tok", 4210, "EUR", "1A2B3C", "M-1", day(11), model.Presentment),
		clearing("C-2", "tok", 4210, "EUR", "1A2B3C", "M-1", day(12), model.Presentment),
	}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["C-1"].Outcome != Matched {
		t.Errorf("C-1 = %+v, want matched", got["C-1"])
	}
	if got["C-2"].Outcome != UnmatchedClearing {
		t.Errorf("C-2 = %+v, want unmatched: the auth was already consumed", got["C-2"])
	}
}

func TestReversalLinksWithoutConsuming(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 3500, "EUR", "8Q9R0S", "M-1", at(11, 21))}
	clearings := []model.Clearing{
		// The reversal sorts first by date but must not steal the auth from the presentment.
		clearing("C-rev", "tok", 3500, "EUR", "8Q9R0S", "M-1", day(12), model.Reversal),
		clearing("C-pres", "tok", 3500, "EUR", "8Q9R0S", "M-1", day(13), model.Presentment),
	}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	for _, ref := range []string{"C-rev", "C-pres"} {
		if got[ref].Outcome != Matched || got[ref].Auth.ID != "A-1" {
			t.Errorf("%s = %+v, want matched to A-1", ref, got[ref])
		}
	}
	if _, open := got["A-1"]; open {
		t.Error("A-1 was reported as open although it settled")
	}
}

func TestChargebackLinksConsumedAuthorization(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 19900, "EUR", "1T2U3V", "M-1", time.Date(2025, 1, 28, 10, 0, 0, 0, time.UTC))}
	clearings := []model.Clearing{
		clearing("C-pres", "tok", 19900, "EUR", "1T2U3V", "M-1", day(1), model.Presentment),
		clearing("C-cb", "tok", 19900, "EUR", "1T2U3V", "M-1", day(13), model.Chargeback),
	}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["C-cb"].Outcome != Matched || got["C-cb"].Auth.ID != "A-1" {
		t.Errorf("chargeback = %+v, want linked to the consumed auth", got["C-cb"])
	}
}

func TestChargebackOnlyAuthIsNotOpen(t *testing.T) {
	// The presentment settled in an earlier file; only the chargeback is in this run.
	auths := []model.Auth{auth("A-1", "tok", 19900, "EUR", "1T2U3V", "M-1", at(1, 10))}
	clearings := []model.Clearing{clearing("C-cb", "tok", 19900, "EUR", "1T2U3V", "M-1", day(13), model.Chargeback)}
	results := run(t, auths, clearings, DefaultOptions())
	if len(results) != 1 || results[0].Outcome != Matched {
		t.Errorf("got %+v, want a single matched chargeback and no open auth", results)
	}
}

func TestUnmatchedClearingAndOpenAuth(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok_a", 6000, "GBP", "5N6O7P", "M-1", at(13, 19))}
	clearings := []model.Clearing{clearing("C-1", "tok_b", 7325, "EUR", "5A5B5C", "M-9", day(13), model.Presentment)}
	got := byRef(run(t, auths, clearings, DefaultOptions()))
	if got["C-1"].Outcome != UnmatchedClearing || got["C-1"].Auth != nil || got["C-1"].Note == "" {
		t.Errorf("C-1 = %+v", got["C-1"])
	}
	if got["A-1"].Outcome != OpenAuth || got["A-1"].Clearing != nil || got["A-1"].Outcome.IsException() {
		t.Errorf("A-1 = %+v", got["A-1"])
	}
}

func TestSettledBeforeAuthorizationIsNoted(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 4210, "EUR", "1A2B3C", "M-1", at(12, 8))}
	clearings := []model.Clearing{clearing("C-1", "tok", 4210, "EUR", "1A2B3C", "M-1", day(11), model.Presentment)}
	r := run(t, auths, clearings, DefaultOptions())[0]
	if r.Outcome != Matched || r.Note != "settled 1 day(s) before the authorization" {
		t.Errorf("got %+v", r)
	}
}

func TestResultsAreSortedByDateThenRef(t *testing.T) {
	auths := []model.Auth{
		auth("A-2", "tok", 100, "EUR", "B", "M", at(9, 9)),
		auth("A-1", "tok", 100, "EUR", "A", "M", at(9, 8)),
	}
	clearings := []model.Clearing{
		clearing("C-2", "tok", 100, "EUR", "B", "M", day(12), model.Presentment),
		clearing("C-1", "tok", 100, "EUR", "A", "M", day(10), model.Presentment),
		clearing("C-0", "tok", 999, "EUR", "Z", "M", day(12), model.Presentment),
	}
	results := run(t, auths, clearings, DefaultOptions())
	var refs []string
	for _, r := range results {
		refs = append(refs, r.Ref())
	}
	want := []string{"C-1", "C-0", "C-2"}
	if !reflect.DeepEqual(refs, want) {
		t.Errorf("order = %v, want %v", refs, want)
	}
}

func TestReconcileIsDeterministicUnderShuffle(t *testing.T) {
	var auths []model.Auth
	var clearings []model.Clearing
	for i := 0; i < 40; i++ {
		code := string(rune('A' + i%5))
		auths = append(auths, auth("A-"+string(rune('a'+i%26))+string(rune('0'+i/26)), "tok", int64(100+i%7), "EUR", code, "M", at(1+i%10, i%24)))
		clearings = append(clearings, clearing("C-"+string(rune('a'+i%26))+string(rune('0'+i/26)), "tok", int64(100+(i+1)%7), "EUR", code, "M", day(2+i%12), model.Presentment))
	}
	baseline := run(t, auths, clearings, DefaultOptions())

	rng := rand.New(rand.NewSource(7))
	for round := 0; round < 5; round++ {
		a := append([]model.Auth(nil), auths...)
		c := append([]model.Clearing(nil), clearings...)
		rng.Shuffle(len(a), func(i, j int) { a[i], a[j] = a[j], a[i] })
		rng.Shuffle(len(c), func(i, j int) { c[i], c[j] = c[j], c[i] })
		got := run(t, a, c, DefaultOptions())
		if len(got) != len(baseline) {
			t.Fatalf("round %d: %d results, want %d", round, len(got), len(baseline))
		}
		for i := range got {
			if got[i].Outcome != baseline[i].Outcome || got[i].Ref() != baseline[i].Ref() || got[i].Tier != baseline[i].Tier {
				t.Fatalf("round %d result %d differs: %+v vs %+v", round, i, got[i], baseline[i])
			}
			if (got[i].Auth == nil) != (baseline[i].Auth == nil) || (got[i].Auth != nil && got[i].Auth.ID != baseline[i].Auth.ID) {
				t.Fatalf("round %d result %d linked a different auth", round, i)
			}
		}
	}
}

func TestAsOfDerivedFromData(t *testing.T) {
	auths := []model.Auth{auth("A-1", "tok", 100, "EUR", "A", "M", at(3, 8))}
	clearings := []model.Clearing{
		clearing("C-1", "tok", 100, "EUR", "A", "M", day(5), model.Presentment),
		clearing("C-2", "tok", 200, "EUR", "B", "M", day(14), model.Presentment),
	}
	rec, err := Reconcile(auths, clearings, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !rec.AsOf.Equal(day(14)) {
		t.Errorf("AsOf = %v, want latest settlement date %v", rec.AsOf, day(14))
	}

	rec, _ = Reconcile(auths, nil, DefaultOptions())
	if !rec.AsOf.Equal(day(3)) {
		t.Errorf("AsOf without clearings = %v, want latest auth day %v", rec.AsOf, day(3))
	}

	rec, _ = Reconcile(nil, nil, DefaultOptions())
	if rec.AsOf.IsZero() || rec.AsOf.Hour() != 0 || len(rec.Results) != 0 {
		t.Errorf("empty run AsOf = %v, results %v", rec.AsOf, rec.Results)
	}

	rec, _ = Reconcile(auths, clearings, Options{AsOf: day(20)})
	if !rec.AsOf.Equal(day(20)) {
		t.Errorf("explicit AsOf ignored: %v", rec.AsOf)
	}
}

func TestOutcomeIsException(t *testing.T) {
	if Matched.IsException() || OpenAuth.IsException() {
		t.Error("matched and open auths are not exceptions")
	}
	if !UnmatchedClearing.IsException() {
		t.Error("unmatched clearing is an exception")
	}
}

func TestDaysBetween(t *testing.T) {
	if got := daysBetween(day(10), day(13)); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
	if got := daysBetween(day(13), day(10)); got != -3 {
		t.Errorf("got %d, want -3", got)
	}
}
