// Package report turns a reconciliation into something people and systems
// can consume: an aligned table for the terminal, JSON for downstream
// tooling and CSV for the spreadsheet the operations team will open anyway.
package report

import (
	"sort"

	"github.com/HelenArams/settlement-recon-cli/internal/match"
	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

// CurrencyNet is the settlement position in one currency, in minor units.
// Reversed and ChargedBack are negative or zero; Net is their sum with
// Presented, i.e. what the scheme will actually settle.
type CurrencyNet struct {
	Currency    string
	Presented   int64
	Reversed    int64
	ChargedBack int64
	Net         int64
}

// Summary aggregates a reconciliation.
type Summary struct {
	Total      int
	Exceptions int
	ByOutcome  map[match.Outcome]int
	ByTier     map[int]int   // 1, 2 and 3
	Net        []CurrencyNet // sorted by currency code
}

// Summarize counts outcomes and tiers and nets the clearing records per
// currency. Duplicates are left out of the net because the scheme settles a
// record once, however many times the file repeats it.
func Summarize(rec *match.Reconciliation) Summary {
	s := Summary{
		Total:     len(rec.Results),
		ByOutcome: make(map[match.Outcome]int, len(match.Outcomes)),
		ByTier:    map[int]int{1: 0, 2: 0, 3: 0},
	}
	for _, o := range match.Outcomes {
		s.ByOutcome[o] = 0
	}
	nets := make(map[string]*CurrencyNet)
	for _, r := range rec.Results {
		s.ByOutcome[r.Outcome]++
		if r.Outcome.IsException() {
			s.Exceptions++
		}
		if r.Tier > 0 {
			s.ByTier[r.Tier]++
		}
		if r.Clearing == nil || r.Outcome == match.Duplicate {
			continue
		}
		c := r.Clearing
		n, ok := nets[c.Currency]
		if !ok {
			n = &CurrencyNet{Currency: c.Currency}
			nets[c.Currency] = n
		}
		switch c.Type {
		case model.Presentment:
			n.Presented += c.SignedAmount()
		case model.Reversal:
			n.Reversed += c.SignedAmount()
		case model.Chargeback:
			n.ChargedBack += c.SignedAmount()
		}
		n.Net += c.SignedAmount()
	}
	s.Net = make([]CurrencyNet, 0, len(nets))
	for _, n := range nets {
		s.Net = append(s.Net, *n)
	}
	sort.Slice(s.Net, func(i, j int) bool { return s.Net[i].Currency < s.Net[j].Currency })
	return s
}
