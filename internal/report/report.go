package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/HelenArams/settlement-recon-cli/internal/match"
	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

// Format selects an output renderer.
type Format string

const (
	Table Format = "table"
	JSON  Format = "json"
	CSV   Format = "csv"
)

var Formats = []Format{Table, JSON, CSV}

func ParseFormat(s string) (Format, error) {
	for _, f := range Formats {
		if strings.EqualFold(s, string(f)) {
			return f, nil
		}
	}
	return "", fmt.Errorf("unknown format %q (want table, json or csv)", s)
}

func Write(w io.Writer, rec *match.Reconciliation, f Format) error {
	switch f {
	case Table:
		return WriteTable(w, rec)
	case JSON:
		return WriteJSON(w, rec)
	case CSV:
		return WriteCSV(w, rec)
	default:
		return fmt.Errorf("unknown format %q", f)
	}
}

const dateLayout = "2006-01-02"

func WriteTable(w io.Writer, rec *match.Reconciliation) error {
	s := Summarize(rec)
	opt := rec.Options

	var b strings.Builder
	fmt.Fprintf(&b, "Settlement reconciliation as of %s\n", rec.AsOf.Format(dateLayout))
	fmt.Fprintf(&b, "tolerance %d bps, window %d days, auth expiry %d days\n\n", opt.ToleranceBps, opt.WindowDays, opt.ExpireDays)

	counts := newTable("OUTCOME", "COUNT", "").alignRight(1)
	for _, o := range match.Outcomes {
		counts.add(string(o), strconv.Itoa(s.ByOutcome[o]), tierDetail(o, s))
	}
	counts.add("TOTAL", strconv.Itoa(s.Total), fmt.Sprintf("exceptions: %d", s.Exceptions))
	if err := counts.render(&b); err != nil {
		return err
	}

	b.WriteString("\nNET SETTLEMENT\n")
	net := newTable("CCY", "PRESENTED", "REVERSED", "CHARGEBACKS", "NET").alignRight(1, 2, 3, 4)
	for _, n := range s.Net {
		net.add(n.Currency,
			model.FormatAmount(n.Presented, n.Currency),
			model.FormatAmount(n.Reversed, n.Currency),
			model.FormatAmount(n.ChargedBack, n.Currency),
			model.FormatAmount(n.Net, n.Currency))
	}
	if len(s.Net) == 0 {
		net.add("-", "-", "-", "-", "-")
	}
	if err := net.render(&b); err != nil {
		return err
	}

	b.WriteString("\n")
	rows := newTable("DATE", "OUTCOME", "TIER", "AUTH ID", "ARN", "TOKEN", "CCY", "AUTHORIZED", "CLEARED", "DELTA", "NOTE").alignRight(7, 8, 9)
	for _, r := range rec.Results {
		rows.add(
			r.Date().Format(dateLayout),
			string(r.Outcome),
			tierLabel(r.Tier),
			authID(r),
			arn(r),
			token(r),
			currency(r),
			authAmount(r),
			clearedAmount(r),
			delta(r),
			r.Note,
		)
	}
	if err := rows.render(&b); err != nil {
		return err
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func tierDetail(o match.Outcome, s Summary) string {
	switch o {
	case match.Matched:
		return fmt.Sprintf("tier 1: %d, tier 2: %d", s.ByTier[1], s.ByTier[2])
	case match.Partial:
		return fmt.Sprintf("tier 3: %d", s.ByTier[3])
	}
	return ""
}

func tierLabel(t int) string {
	if t == 0 {
		return "-"
	}
	return "T" + strconv.Itoa(t)
}

func authID(r match.Result) string {
	if r.Auth == nil {
		return "-"
	}
	return r.Auth.ID
}

func arn(r match.Result) string {
	if r.Clearing == nil {
		return "-"
	}
	return r.Clearing.ARN
}

func token(r match.Result) string {
	if r.Clearing != nil {
		return r.Clearing.CardToken
	}
	return r.Auth.CardToken
}

func currency(r match.Result) string {
	if r.Clearing != nil {
		return r.Clearing.Currency
	}
	return r.Auth.Currency
}

func authAmount(r match.Result) string {
	if r.Auth == nil {
		return "-"
	}
	return model.FormatAmount(r.Auth.Amount, r.Auth.Currency)
}

func clearedAmount(r match.Result) string {
	if r.Clearing == nil {
		return "-"
	}
	return model.FormatAmount(r.Clearing.SignedAmount(), r.Clearing.Currency)
}

// delta renders the signed difference between cleared and authorized amount,
// with an explicit plus sign so a column of deltas scans at a glance.
func delta(r match.Result) string {
	if r.Auth == nil || r.Clearing == nil {
		return ""
	}
	s := model.FormatAmount(r.Delta, r.Auth.Currency)
	if r.Delta > 0 {
		s = "+" + s
	}
	return s
}

type jsonReport struct {
	AsOf    string       `json:"as_of"`
	Options jsonOptions  `json:"options"`
	Summary jsonSummary  `json:"summary"`
	Results []jsonResult `json:"results"`
}

type jsonOptions struct {
	ToleranceBps int `json:"tolerance_bps"`
	WindowDays   int `json:"window_days"`
	ExpireDays   int `json:"expire_days"`
}

type jsonSummary struct {
	Total         int            `json:"total"`
	Exceptions    int            `json:"exceptions"`
	Outcomes      map[string]int `json:"outcomes"`
	Tiers         map[string]int `json:"tiers"`
	NetSettlement []jsonNet      `json:"net_settlement"`
}

type jsonNet struct {
	Currency    string `json:"currency"`
	Presented   string `json:"presented"`
	Reversed    string `json:"reversed"`
	Chargebacks string `json:"chargebacks"`
	Net         string `json:"net"`
}

type jsonResult struct {
	Outcome  string        `json:"outcome"`
	Tier     int           `json:"tier,omitempty"`
	Auth     *jsonAuth     `json:"auth"`
	Clearing *jsonClearing `json:"clearing"`
	Delta    string        `json:"delta,omitempty"`
	Note     string        `json:"note,omitempty"`
}

type jsonAuth struct {
	ID         string `json:"id"`
	CardToken  string `json:"card_token"`
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	AuthCode   string `json:"auth_code,omitempty"`
	MerchantID string `json:"merchant_id"`
	Timestamp  string `json:"timestamp"`
}

type jsonClearing struct {
	ARN            string `json:"arn"`
	CardToken      string `json:"card_token"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
	AuthCode       string `json:"auth_code,omitempty"`
	MerchantID     string `json:"merchant_id"`
	SettlementDate string `json:"settlement_date"`
	Type           string `json:"type"`
}

// Amounts are decimal strings in the currency's exponent so nobody downstream
// has to guess the scale.
func WriteJSON(w io.Writer, rec *match.Reconciliation) error {
	s := Summarize(rec)
	out := jsonReport{
		AsOf: rec.AsOf.Format(dateLayout),
		Options: jsonOptions{
			ToleranceBps: rec.Options.ToleranceBps,
			WindowDays:   rec.Options.WindowDays,
			ExpireDays:   rec.Options.ExpireDays,
		},
		Summary: jsonSummary{
			Total:         s.Total,
			Exceptions:    s.Exceptions,
			Outcomes:      make(map[string]int, len(s.ByOutcome)),
			Tiers:         make(map[string]int, len(s.ByTier)),
			NetSettlement: make([]jsonNet, 0, len(s.Net)),
		},
		Results: make([]jsonResult, 0, len(rec.Results)),
	}
	for o, n := range s.ByOutcome {
		out.Summary.Outcomes[string(o)] = n
	}
	for t, n := range s.ByTier {
		out.Summary.Tiers[strconv.Itoa(t)] = n
	}
	for _, n := range s.Net {
		out.Summary.NetSettlement = append(out.Summary.NetSettlement, jsonNet{
			Currency:    n.Currency,
			Presented:   model.FormatAmount(n.Presented, n.Currency),
			Reversed:    model.FormatAmount(n.Reversed, n.Currency),
			Chargebacks: model.FormatAmount(n.ChargedBack, n.Currency),
			Net:         model.FormatAmount(n.Net, n.Currency),
		})
	}
	for _, r := range rec.Results {
		jr := jsonResult{Outcome: string(r.Outcome), Tier: r.Tier, Note: r.Note}
		if r.Auth != nil {
			a := r.Auth
			jr.Auth = &jsonAuth{
				ID: a.ID, CardToken: a.CardToken, Amount: model.FormatAmount(a.Amount, a.Currency),
				Currency: a.Currency, AuthCode: a.AuthCode, MerchantID: a.MerchantID,
				Timestamp: a.Timestamp.UTC().Format(time.RFC3339),
			}
		}
		if r.Clearing != nil {
			c := r.Clearing
			jr.Clearing = &jsonClearing{
				ARN: c.ARN, CardToken: c.CardToken, Amount: model.FormatAmount(c.Amount, c.Currency),
				Currency: c.Currency, AuthCode: c.AuthCode, MerchantID: c.MerchantID,
				SettlementDate: c.SettlementDate.Format(dateLayout), Type: string(c.Type),
			}
		}
		if r.Auth != nil && r.Clearing != nil {
			jr.Delta = model.FormatAmount(r.Delta, r.Auth.Currency)
		}
		out.Results = append(out.Results, jr)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// csvHeader is the column order of the CSV report. Both merchant identifiers
// are kept because tier-1 matches tolerate a remap between the two sides.
var csvHeader = []string{
	"date", "outcome", "tier", "auth_id", "arn", "card_token", "currency",
	"authorized", "cleared", "delta", "clearing_type",
	"auth_merchant_id", "clearing_merchant_id", "auth_timestamp", "settlement_date", "note",
}

func WriteCSV(w io.Writer, rec *match.Reconciliation) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, r := range rec.Results {
		row := []string{
			r.Date().Format(dateLayout),
			string(r.Outcome),
			strconv.Itoa(r.Tier),
			"", "", token(r), currency(r),
			"", "", "", "",
			"", "", "", "",
			r.Note,
		}
		if r.Auth != nil {
			row[3] = r.Auth.ID
			row[7] = model.FormatAmount(r.Auth.Amount, r.Auth.Currency)
			row[11] = r.Auth.MerchantID
			row[13] = r.Auth.Timestamp.UTC().Format(time.RFC3339)
		}
		if r.Clearing != nil {
			row[4] = r.Clearing.ARN
			row[8] = model.FormatAmount(r.Clearing.SignedAmount(), r.Clearing.Currency)
			row[10] = string(r.Clearing.Type)
			row[12] = r.Clearing.MerchantID
			row[14] = r.Clearing.SettlementDate.Format(dateLayout)
		}
		if r.Auth != nil && r.Clearing != nil {
			row[9] = model.FormatAmount(r.Delta, r.Auth.Currency)
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
