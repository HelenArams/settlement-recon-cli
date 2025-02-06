// Package match pairs issuer authorizations with scheme clearing records.
//
// Matching runs in tiers, strictest first:
//
//	tier 1  same card, currency, approval code and amount
//	tier 2  same card, currency and merchant; amount within a tolerance in
//	        basis points; settled within a window of days
//	tier 3  same card, currency and merchant within the window, any amount;
//	        reported as PARTIAL because the money does not agree
//
// A presentment consumes the authorization it matches so no two captures
// settle against the same hold; reversals and chargebacks only link back to
// the authorization they adjust, because they always follow a presentment of
// their own. A clearing record whose ARN was already seen is a DUPLICATE and
// takes no part in matching. Authorizations nothing settled against are OPEN
// until they are older than the expiry, then EXPIRED. Processing order is
// fixed (settlement date, then ARN) so the same input always yields the same
// report regardless of file order.
package match

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

// Outcome is the reconciliation verdict for one record.
type Outcome string

const (
	// Matched means the clearing record was paired with an authorization
	// whose amount agrees, exactly or within tolerance.
	Matched Outcome = "MATCHED"
	// Partial means an authorization was found but the amount differs
	// beyond tolerance: partial captures, tips, FX swings past the limit.
	Partial Outcome = "PARTIAL"
	// UnmatchedClearing means money settled that no authorization explains.
	UnmatchedClearing Outcome = "UNMATCHED_CLEARING"
	// ExpiredAuth is an authorization that nothing settled against within
	// the expiry period: the hold should be released.
	ExpiredAuth Outcome = "EXPIRED_AUTH"
	// Duplicate is a clearing record whose ARN already appeared in the file.
	Duplicate Outcome = "DUPLICATE"
	// OpenAuth is an authorization still waiting for its presentment.
	OpenAuth Outcome = "OPEN_AUTH"
)

// Outcomes lists every outcome in report order.
var Outcomes = []Outcome{Matched, Partial, UnmatchedClearing, ExpiredAuth, Duplicate, OpenAuth}

// IsException reports whether an outcome needs a human to look at it. Open
// authorizations are normal between capture and settlement, so they are not.
func (o Outcome) IsException() bool {
	return o != Matched && o != OpenAuth
}

// Options tune the matcher.
type Options struct {
	// ToleranceBps is how far a cleared amount may drift from the authorized
	// amount and still count as MATCHED at tier 2, in basis points of the
	// authorized amount (50 = 0.5%). Covers FX rate movement between
	// authorization and clearing on cross-currency transactions.
	ToleranceBps int
	// WindowDays is the largest gap, in calendar days, between the
	// authorization and the settlement date for tiers 2 and 3.
	WindowDays int
	// ExpireDays is how many days an authorization may stay open before it
	// is reported as EXPIRED_AUTH, measured from its day to AsOf.
	ExpireDays int
	// AsOf is the reconciliation date. Zero means "the latest settlement
	// date in the clearing file", which keeps batch runs reproducible.
	AsOf time.Time
}

// DefaultOptions are sensible for a daily issuer-side run.
func DefaultOptions() Options {
	return Options{ToleranceBps: 50, WindowDays: 3, ExpireDays: 7}
}

// Validate rejects option values that cannot be meant.
func (o Options) Validate() error {
	var problems []string
	if o.ToleranceBps < 0 || o.ToleranceBps > 10_000 {
		problems = append(problems, fmt.Sprintf("tolerance must be between 0 and 10000 bps, got %d", o.ToleranceBps))
	}
	if o.WindowDays < 0 {
		problems = append(problems, fmt.Sprintf("window must not be negative, got %d days", o.WindowDays))
	}
	if o.ExpireDays < 0 {
		problems = append(problems, fmt.Sprintf("expiry must not be negative, got %d days", o.ExpireDays))
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid options: %s", strings.Join(problems, "; "))
	}
	return nil
}

// Result is the verdict for one clearing record or one unmatched
// authorization. Auth and Clearing point into the slices given to Reconcile.
type Result struct {
	Outcome  Outcome
	Tier     int // 1..3 for the tier that matched; 0 when no match was made
	Auth     *model.Auth
	Clearing *model.Clearing
	Delta    int64  // Clearing.Amount - Auth.Amount, when both are present
	Note     string // short human explanation, empty for a clean match
}

// Date is the day a result is reported under: the settlement date when a
// clearing record is involved, otherwise the authorization date.
func (r Result) Date() time.Time {
	if r.Clearing != nil {
		return r.Clearing.SettlementDate
	}
	return r.Auth.Date()
}

// Ref is the record identifier a result is reported under: the ARN when a
// clearing record is involved, otherwise the authorization ID.
func (r Result) Ref() string {
	if r.Clearing != nil {
		return r.Clearing.ARN
	}
	return r.Auth.ID
}

// Reconciliation is the full outcome of one run.
type Reconciliation struct {
	AsOf    time.Time
	Options Options
	Results []Result // sorted by Date, then Ref
}

// Reconcile matches clearings against auths. The input slices are indexed,
// not copied, so they must not be modified while the result is in use.
func Reconcile(auths []model.Auth, clearings []model.Clearing, opt Options) (*Reconciliation, error) {
	if err := opt.Validate(); err != nil {
		return nil, err
	}
	asOf := opt.AsOf
	if asOf.IsZero() {
		asOf = deriveAsOf(auths, clearings)
	}
	m := newMatcher(auths, opt)

	results := make([]Result, 0, len(auths)+len(clearings))
	seen := make(map[string]struct{}, len(clearings))
	var adjustments []int // reversals and chargebacks, linked once presentments are placed
	for _, ci := range clearingOrder(clearings) {
		c := &clearings[ci]
		if _, dup := seen[c.ARN]; dup {
			results = append(results, Result{Outcome: Duplicate, Clearing: c, Note: "ARN already present in the clearing file"})
			continue
		}
		seen[c.ARN] = struct{}{}
		if c.Type != model.Presentment {
			adjustments = append(adjustments, ci)
			continue
		}
		results = append(results, m.resolve(c))
	}
	for _, ci := range adjustments {
		results = append(results, m.resolve(&clearings[ci]))
	}
	for i := range auths {
		if m.linked[i] {
			continue
		}
		a := &auths[i]
		if age := daysBetween(a.Date(), asOf); age > opt.ExpireDays {
			note := fmt.Sprintf("open for %d days, expiry is %d", age, opt.ExpireDays)
			results = append(results, Result{Outcome: ExpiredAuth, Auth: a, Note: note})
			continue
		}
		results = append(results, Result{Outcome: OpenAuth, Auth: a})
	}
	sortResults(results)
	return &Reconciliation{AsOf: asOf, Options: opt, Results: results}, nil
}

// exactKey identifies a tier-1 candidate: same card, currency, approval code
// and amount. The merchant is deliberately absent because acquirers remap
// merchant identifiers between authorization and clearing.
type exactKey struct {
	token, currency, authCode string
	amount                    int64
}

// cardKey narrows tiers 2 and 3 to the same card and currency.
type cardKey struct {
	token, currency string
}

type matcher struct {
	opt      Options
	auths    []model.Auth
	byExact  map[exactKey][]int // candidate indexes in chronological order
	byCard   map[cardKey][]int
	consumed []bool // a presentment has settled against this auth
	linked   []bool // any clearing record references this auth
}

func newMatcher(auths []model.Auth, opt Options) *matcher {
	m := &matcher{
		opt:      opt,
		auths:    auths,
		byExact:  make(map[exactKey][]int, len(auths)),
		byCard:   make(map[cardKey][]int, len(auths)),
		consumed: make([]bool, len(auths)),
		linked:   make([]bool, len(auths)),
	}
	for _, i := range authOrder(auths) {
		a := &auths[i]
		if a.AuthCode != "" {
			k := exactKey{a.CardToken, a.Currency, a.AuthCode, a.Amount}
			m.byExact[k] = append(m.byExact[k], i)
		}
		ck := cardKey{a.CardToken, a.Currency}
		m.byCard[ck] = append(m.byCard[ck], i)
	}
	return m
}

// resolve finds the best authorization for c and records the link.
func (m *matcher) resolve(c *model.Clearing) Result {
	consuming := c.Type == model.Presentment
	i, tier, ok := m.find(c, consuming)
	if !ok {
		return Result{Outcome: UnmatchedClearing, Clearing: c, Note: "no authorization for this card and merchant within the window"}
	}
	a := &m.auths[i]
	m.linked[i] = true
	if consuming {
		m.consumed[i] = true
	}
	r := Result{Outcome: Matched, Tier: tier, Auth: a, Clearing: c, Delta: c.Amount - a.Amount}
	if tier == 3 {
		r.Outcome = Partial
	}
	r.Note = m.note(a, c, tier)
	return r
}

func (m *matcher) find(c *model.Clearing, consuming bool) (idx, tier int, ok bool) {
	if c.AuthCode != "" {
		if i, ok := m.tier1(c, consuming); ok {
			return i, 1, true
		}
	}
	if i, ok := m.tier2(c, consuming); ok {
		return i, 2, true
	}
	if i, ok := m.tier3(c, consuming); ok {
		return i, 3, true
	}
	return -1, 0, false
}

// tier1 is an exact match on card, currency, approval code and amount. When
// several authorizations qualify (a repeated purchase with a recycled code),
// the one settled closest to its authorization date wins.
func (m *matcher) tier1(c *model.Clearing, consuming bool) (int, bool) {
	best, bestDays := -1, 0
	for _, i := range m.byExact[exactKey{c.CardToken, c.Currency, c.AuthCode, c.Amount}] {
		if !m.available(i, consuming) {
			continue
		}
		days := absInt(m.daysApart(i, c))
		if best < 0 || days < bestDays {
			best, bestDays = i, days
		}
	}
	return best, best >= 0
}

// tier2 matches the same card and merchant within the date window when the
// amount is inside the tolerance. The closest amount wins, then the closest
// date.
func (m *matcher) tier2(c *model.Clearing, consuming bool) (int, bool) {
	best, bestDelta, bestDays := -1, int64(0), 0
	for _, i := range m.merchantCandidates(c, consuming) {
		a := &m.auths[i]
		if !model.WithinBps(a.Amount, c.Amount, m.opt.ToleranceBps) {
			continue
		}
		delta, days := absInt64(c.Amount-a.Amount), absInt(m.daysApart(i, c))
		if best < 0 || delta < bestDelta || (delta == bestDelta && days < bestDays) {
			best, bestDelta, bestDays = i, delta, days
		}
	}
	return best, best >= 0
}

// tier3 matches the same card and merchant within the date window regardless
// of amount. The closest date wins, then the closest amount.
func (m *matcher) tier3(c *model.Clearing, consuming bool) (int, bool) {
	best, bestDelta, bestDays := -1, int64(0), 0
	for _, i := range m.merchantCandidates(c, consuming) {
		delta, days := absInt64(c.Amount-m.auths[i].Amount), absInt(m.daysApart(i, c))
		if best < 0 || days < bestDays || (days == bestDays && delta < bestDelta) {
			best, bestDelta, bestDays = i, delta, days
		}
	}
	return best, best >= 0
}

// merchantCandidates returns the authorizations on the same card, currency
// and merchant whose date is within the window of c, in chronological order.
func (m *matcher) merchantCandidates(c *model.Clearing, consuming bool) []int {
	var out []int
	for _, i := range m.byCard[cardKey{c.CardToken, c.Currency}] {
		if !m.available(i, consuming) || m.auths[i].MerchantID != c.MerchantID {
			continue
		}
		if absInt(m.daysApart(i, c)) > m.opt.WindowDays {
			continue
		}
		out = append(out, i)
	}
	return out
}

// available reports whether auth i may still be matched by a record that
// would consume it. Non-consuming records may link to any authorization.
func (m *matcher) available(i int, consuming bool) bool {
	return !consuming || !m.consumed[i]
}

// daysApart is the signed number of days from the authorization day of auth
// i to the settlement date of c.
func (m *matcher) daysApart(i int, c *model.Clearing) int {
	return daysBetween(m.auths[i].Date(), c.SettlementDate)
}

// note explains anything about a match that a reviewer would want to know.
func (m *matcher) note(a *model.Auth, c *model.Clearing, tier int) string {
	var parts []string
	if delta := c.Amount - a.Amount; delta != 0 {
		sign := "+"
		if delta < 0 {
			sign = "-"
		}
		part := fmt.Sprintf("amount off by %s%s (%d bps)", sign, model.FormatAmount(absInt64(delta), a.Currency), model.Bps(a.Amount, c.Amount))
		if tier == 3 {
			part += fmt.Sprintf(", outside %d bps tolerance", m.opt.ToleranceBps)
		}
		parts = append(parts, part)
	}
	days := daysBetween(a.Date(), c.SettlementDate)
	switch {
	case days < 0:
		parts = append(parts, fmt.Sprintf("settled %d day(s) before the authorization", -days))
	case days > m.opt.WindowDays:
		parts = append(parts, fmt.Sprintf("settled %d days after auth, outside %d-day window", days, m.opt.WindowDays))
	}
	if tier == 1 && a.MerchantID != c.MerchantID {
		parts = append(parts, fmt.Sprintf("merchant differs: %s vs %s", a.MerchantID, c.MerchantID))
	}
	if tier > 1 && c.AuthCode != "" && a.AuthCode != "" && c.AuthCode != a.AuthCode {
		parts = append(parts, fmt.Sprintf("auth code differs: %s vs %s", a.AuthCode, c.AuthCode))
	}
	return strings.Join(parts, "; ")
}

// deriveAsOf picks the reconciliation date from the data: the latest
// settlement date, failing that the latest authorization day, failing that
// today. Deriving it from the clearing file makes reruns reproducible.
func deriveAsOf(auths []model.Auth, clearings []model.Clearing) time.Time {
	var asOf time.Time
	for i := range clearings {
		if d := clearings[i].SettlementDate; d.After(asOf) {
			asOf = d
		}
	}
	if !asOf.IsZero() {
		return asOf
	}
	for i := range auths {
		if d := auths[i].Date(); d.After(asOf) {
			asOf = d
		}
	}
	if !asOf.IsZero() {
		return asOf
	}
	y, mo, d := time.Now().UTC().Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
}

// authOrder returns auth indexes sorted by timestamp, then ID, so candidate
// lists do not depend on the input file order.
func authOrder(auths []model.Auth) []int {
	order := make([]int, len(auths))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool {
		a, b := &auths[order[x]], &auths[order[y]]
		if !a.Timestamp.Equal(b.Timestamp) {
			return a.Timestamp.Before(b.Timestamp)
		}
		return a.ID < b.ID
	})
	return order
}

// clearingOrder returns clearing indexes sorted by settlement date, then ARN.
func clearingOrder(clearings []model.Clearing) []int {
	order := make([]int, len(clearings))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool {
		a, b := &clearings[order[x]], &clearings[order[y]]
		if !a.SettlementDate.Equal(b.SettlementDate) {
			return a.SettlementDate.Before(b.SettlementDate)
		}
		return a.ARN < b.ARN
	})
	return order
}

func sortResults(results []Result) {
	sort.SliceStable(results, func(i, j int) bool {
		di, dj := results[i].Date(), results[j].Date()
		if !di.Equal(dj) {
			return di.Before(dj)
		}
		if ri, rj := results[i].Ref(), results[j].Ref(); ri != rj {
			return ri < rj
		}
		return outcomeRank(results[i].Outcome) < outcomeRank(results[j].Outcome)
	})
}

func outcomeRank(o Outcome) int {
	for i, candidate := range Outcomes {
		if candidate == o {
			return i
		}
	}
	return len(Outcomes)
}

// daysBetween returns the number of calendar days from a to b. Both values
// are expected at UTC midnight, as produced by Auth.Date and the loader.
func daysBetween(a, b time.Time) int {
	return int(b.Sub(a) / (24 * time.Hour))
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
