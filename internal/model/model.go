// Package model defines the records a reconciliation works with: issuer-side
// authorizations and the clearing records a card scheme sends back.
//
// Amounts are kept in minor units (cents, pence, fils) as int64 so that
// arithmetic is exact. Use the helpers in money.go to move between the
// decimal strings found in CSV files and minor units.
package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ClearingType says what a clearing record does to the settlement position.
type ClearingType string

const (
	// Presentment is the capture of an authorization: money the issuer owes.
	Presentment ClearingType = "presentment"
	// Reversal cancels a previous presentment, fully or in part.
	Reversal ClearingType = "reversal"
	// Chargeback is a dispute that pulls a settled presentment back.
	Chargeback ClearingType = "chargeback"
)

// ClearingTypes lists every type in a stable order.
var ClearingTypes = []ClearingType{Presentment, Reversal, Chargeback}

// clearingTypeAliases maps the spellings seen in scheme exports to a type.
var clearingTypeAliases = map[string]ClearingType{
	"presentment":          Presentment,
	"first_presentment":    Presentment,
	"first presentment":    Presentment,
	"presentment_1":        Presentment,
	"sale":                 Presentment,
	"purchase":             Presentment,
	"capture":              Presentment,
	"reversal":             Reversal,
	"presentment_reversal": Reversal,
	"reversal_1":           Reversal,
	"chargeback":           Chargeback,
	"first_chargeback":     Chargeback,
	"chargeback_1":         Chargeback,
	"dispute":              Chargeback,
}

// ParseClearingType maps a raw type cell to a ClearingType. Matching is
// case-insensitive and tolerant of the common scheme spellings.
func ParseClearingType(s string) (ClearingType, error) {
	key := strings.ToLower(strings.TrimSpace(s))
	if t, ok := clearingTypeAliases[key]; ok {
		return t, nil
	}
	return "", fmt.Errorf("unknown clearing type %q", s)
}

// Sign is +1 for records that increase what the issuer owes the scheme and
// -1 for records that reduce it.
func (t ClearingType) Sign() int64 {
	if t == Presentment {
		return 1
	}
	return -1
}

// Auth is an approved authorization as recorded by the issuer.
type Auth struct {
	ID         string
	CardToken  string
	Amount     int64 // minor units, never negative
	Currency   string
	AuthCode   string // may be empty when the processor did not keep it
	MerchantID string
	Timestamp  time.Time // UTC
}

// Date returns the calendar day of the authorization in UTC.
func (a Auth) Date() time.Time {
	return dateOf(a.Timestamp)
}

// Validate checks the invariants every loaded authorization must satisfy.
func (a Auth) Validate() error {
	var errs []error
	if a.ID == "" {
		errs = append(errs, errors.New("id is empty"))
	}
	if a.CardToken == "" {
		errs = append(errs, errors.New("card_token is empty"))
	}
	if a.Amount < 0 {
		errs = append(errs, errors.New("amount is negative"))
	}
	if err := ValidateCurrency(a.Currency); err != nil {
		errs = append(errs, err)
	}
	if a.MerchantID == "" {
		errs = append(errs, errors.New("merchant_id is empty"))
	}
	if a.Timestamp.IsZero() {
		errs = append(errs, errors.New("timestamp is missing"))
	}
	return errors.Join(errs...)
}

// Clearing is one record from a scheme clearing file.
type Clearing struct {
	ARN            string // acquirer reference number, unique per record
	CardToken      string
	Amount         int64 // minor units, never negative; the type carries the sign
	Currency       string
	AuthCode       string // may be empty: acquirers routinely drop it
	MerchantID     string
	SettlementDate time.Time // UTC midnight
	Type           ClearingType
}

// SignedAmount applies the type's sign: positive for presentments, negative
// for reversals and chargebacks.
func (c Clearing) SignedAmount() int64 {
	return c.Type.Sign() * c.Amount
}

// Validate checks the invariants every loaded clearing record must satisfy.
func (c Clearing) Validate() error {
	var errs []error
	if c.ARN == "" {
		errs = append(errs, errors.New("arn is empty"))
	}
	if c.CardToken == "" {
		errs = append(errs, errors.New("card_token is empty"))
	}
	if c.Amount < 0 {
		errs = append(errs, errors.New("amount is negative"))
	}
	if err := ValidateCurrency(c.Currency); err != nil {
		errs = append(errs, err)
	}
	if c.MerchantID == "" {
		errs = append(errs, errors.New("merchant_id is empty"))
	}
	if c.SettlementDate.IsZero() {
		errs = append(errs, errors.New("settlement_date is missing"))
	}
	if _, err := ParseClearingType(string(c.Type)); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ValidateCurrency accepts ISO 4217 alphabetic codes: three upper-case letters.
func ValidateCurrency(ccy string) error {
	if len(ccy) != 3 {
		return fmt.Errorf("currency %q is not a 3-letter ISO code", ccy)
	}
	for _, r := range ccy {
		if r < 'A' || r > 'Z' {
			return fmt.Errorf("currency %q is not a 3-letter ISO code", ccy)
		}
	}
	return nil
}

func dateOf(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
