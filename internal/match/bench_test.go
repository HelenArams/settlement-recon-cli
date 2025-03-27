package match

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

// synth builds n auths and roughly n clearing records spread over ~20k cards,
// with a realistic mix: mostly exact matches, some missing auth codes, some
// FX drift, a few partial captures, duplicates and strays.
func synth(n int) ([]model.Auth, []model.Clearing) {
	rng := rand.New(rand.NewSource(42))
	base := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	cards := n/5 + 1
	auths := make([]model.Auth, 0, n)
	clearings := make([]model.Clearing, 0, n+n/50)
	for i := 0; i < n; i++ {
		a := model.Auth{
			ID:         fmt.Sprintf("A-%07d", i),
			CardToken:  fmt.Sprintf("tok_%06d", rng.Intn(cards)),
			Amount:     int64(100 + rng.Intn(50_000)),
			Currency:   "EUR",
			AuthCode:   fmt.Sprintf("%06X", rng.Intn(1<<24)),
			MerchantID: fmt.Sprintf("M-%04d", rng.Intn(2_000)),
			Timestamp:  base.Add(time.Duration(rng.Intn(14*24)) * time.Hour),
		}
		auths = append(auths, a)

		roll := rng.Intn(100)
		if roll < 5 {
			continue // still open
		}
		c := model.Clearing{
			ARN:            fmt.Sprintf("740%020d", i),
			CardToken:      a.CardToken,
			Amount:         a.Amount,
			Currency:       a.Currency,
			AuthCode:       a.AuthCode,
			MerchantID:     a.MerchantID,
			SettlementDate: a.Date().AddDate(0, 0, 1+rng.Intn(2)),
			Type:           model.Presentment,
		}
		switch {
		case roll < 15:
			c.AuthCode = ""
		case roll < 20:
			c.Amount += c.Amount / 400 // 25 bps
		case roll < 23:
			c.Amount = c.Amount * 7 / 10
		case roll < 24:
			c.CardToken = "tok_stray"
		}
		clearings = append(clearings, c)
		if roll == 99 {
			clearings = append(clearings, c)
		}
	}
	return auths, clearings
}

func BenchmarkReconcile100k(b *testing.B) {
	auths, clearings := synth(100_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Reconcile(auths, clearings, DefaultOptions()); err != nil {
			b.Fatal(err)
		}
	}
}

// Guards against an accidental O(n²): 100k records must reconcile quickly
// even under -race.
func TestReconcileLargeInputIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("large input")
	}
	auths, clearings := synth(100_000)
	start := time.Now()
	rec, err := Reconcile(auths, clearings, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("took %s", elapsed)
	}
	counts := map[Outcome]int{}
	for _, r := range rec.Results {
		counts[r.Outcome]++
	}
	if counts[Matched] < 90_000 || counts[Duplicate] == 0 || counts[Partial] == 0 || counts[UnmatchedClearing] == 0 {
		t.Errorf("unexpected mix: %v", counts)
	}
}
