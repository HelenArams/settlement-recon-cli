// Package match pairs issuer authorizations with scheme clearing records.
//
// Matching runs in tiers, strictest first. A presentment consumes the
// authorization it matches so no two captures settle against the same hold;
// reversals and chargebacks only link back to the authorization they adjust,
// because they always follow a presentment of their own. Processing order is
// fixed (settlement date, then ARN) so the same input always yields the same
// report regardless of how the files were sorted.
package match

import (
	"fmt"
	"sort"
	"time"

	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

// Outcome is the reconciliation verdict for one record.
type Outcome string

const (
	// Matched means the clearing record was paired with an authorization.
	Matched Outcome = "MATCHED"
	// UnmatchedClearing means money settled that no authorization explains.
	UnmatchedClearing Outcome = "UNMATCHED_CLEARING"
	// OpenAuth is an authorization still waiting for its presentment.
	OpenAuth Outcome = "OPEN_AUTH"
)

// Outcomes lists every outcome in report order.
var Outcomes = []Outcome{Matched, UnmatchedClearing, OpenAuth}

// IsException reports whether an outcome needs a human to look at it. Open
// authorizations are normal between capture and settlement, so they are not.
func (o Outcome) IsException() bool {
	return o != Matched && o != OpenAuth
}

// Options tune the matcher.
type Options struct {
	// AsOf is the reconciliation date. Zero means "the latest settlement
	// date in the clearing file", which keeps batch runs reproducible.
	AsOf time.Time
}

// DefaultOptions are sensible for a daily issuer-side run.
func DefaultOptions() Options {
	return Options{}
}

// Validate rejects option values that cannot be meant.
func (o Options) Validate() error {
	return nil
}

// Result is the verdict for one clearing record or one unmatched
// authorization. Auth and Clearing point into the slices given to Reconcile.
type Result struct {
	Outcome  Outcome
	Tier     int // 1 for an exact match; 0 when no match was made
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
	var adjustments []int // reversals and chargebacks, linked once presentments are placed
	for _, ci := range clearingOrder(clearings) {
		c := &clearings[ci]
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
		results = append(results, Result{Outcome: OpenAuth, Auth: &auths[i]})
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

type matcher struct {
	opt      Options
	auths    []model.Auth
	byExact  map[exactKey][]int // candidate indexes in chronological order
	consumed []bool             // a presentment has settled against this auth
	linked   []bool             // any clearing record references this auth
}

func newMatcher(auths []model.Auth, opt Options) *matcher {
	m := &matcher{
		opt:      opt,
		auths:    auths,
		byExact:  make(map[exactKey][]int, len(auths)),
		consumed: make([]bool, len(auths)),
		linked:   make([]bool, len(auths)),
	}
	for _, i := range authOrder(auths) {
		a := &auths[i]
		if a.AuthCode != "" {
			k := exactKey{a.CardToken, a.Currency, a.AuthCode, a.Amount}
			m.byExact[k] = append(m.byExact[k], i)
		}
	}
	return m
}

// resolve finds the best authorization for c and records the link.
func (m *matcher) resolve(c *model.Clearing) Result {
	consuming := c.Type == model.Presentment
	i, tier, ok := m.find(c, consuming)
	if !ok {
		return Result{Outcome: UnmatchedClearing, Clearing: c, Note: "no authorization found for this card and amount"}
	}
	a := &m.auths[i]
	m.linked[i] = true
	if consuming {
		m.consumed[i] = true
	}
	r := Result{Outcome: Matched, Tier: tier, Auth: a, Clearing: c, Delta: c.Amount - a.Amount}
	r.Note = m.note(a, c)
	return r
}

func (m *matcher) find(c *model.Clearing, consuming bool) (idx, tier int, ok bool) {
	if c.AuthCode != "" {
		if i, ok := m.tier1(c, consuming); ok {
			return i, 1, true
		}
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
		days := absInt(daysBetween(m.auths[i].Date(), c.SettlementDate))
		if best < 0 || days < bestDays {
			best, bestDays = i, days
		}
	}
	return best, best >= 0
}

// available reports whether auth i may still be matched by a record that
// would consume it. Non-consuming records may link to any authorization.
func (m *matcher) available(i int, consuming bool) bool {
	return !consuming || !m.consumed[i]
}

func (m *matcher) note(a *model.Auth, c *model.Clearing) string {
	days := daysBetween(a.Date(), c.SettlementDate)
	if days < 0 {
		return fmt.Sprintf("settled %d day(s) before the authorization", -days)
	}
	return ""
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
