package load

import (
	"strings"
	"testing"

	"github.com/HelenArams/settlement-recon-cli/internal/model"
)

// Scheme portals and Excel produce files with a UTF-8 BOM, CRLF line endings
// and whitespace around header cells and values. All of that must load as if
// the file were clean.
func TestAuthsAcceptsBOMAndCRLF(t *testing.T) {
	in := "\xEF\xBB\xBFid,card_token,amount,currency,auth_code,merchant_id,timestamp\r\n" +
		"A-1,tok_1,42.10,EUR,1A2B3C,M-1,2025-02-10T08:15:00Z\r\n" +
		"A-2,tok_2,12.00,EUR,,M-2,2025-02-10T09:15:00Z\r\n"
	got, err := Auths(strings.NewReader(in), "bom.csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0].ID != "A-1" || got[1].Timestamp.IsZero() {
		t.Errorf("got %+v", got)
	}
}

func TestClearingsAcceptsBOMAndCRLF(t *testing.T) {
	in := "\xEF\xBB\xBFarn,card_token,amount,currency,merchant_id,settlement_date,type\r\n" +
		"74000000000000000000001,tok_1,42.10,EUR,M-1,20250211,presentment\r\n"
	got, err := Clearings(strings.NewReader(in), "bom.csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ARN != "74000000000000000000001" || got[0].Type != model.Presentment {
		t.Errorf("got %+v", got)
	}
}

func TestPaddedHeadersAndValues(t *testing.T) {
	in := " ID , Card Token , Amount , Currency , Auth Code , Merchant ID , Timestamp \n" +
		" A-1 , tok_1 , 42.10 , eur , 1a2b3c , M-1 , 2025-02-10T08:15:00Z \n"
	got, err := Auths(strings.NewReader(in), "padded.csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := model.Auth{
		ID: "A-1", CardToken: "tok_1", Amount: 4210, Currency: "EUR", AuthCode: "1A2B3C",
		MerchantID: "M-1", Timestamp: got[0].Timestamp,
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestBOMOnlyFileIsEmpty(t *testing.T) {
	_, err := Auths(strings.NewReader("\xEF\xBB\xBF"), "bom.csv")
	if err == nil || !strings.Contains(err.Error(), "file is empty") {
		t.Fatalf("got %v, want empty-file error", err)
	}
}

func TestShortInputWithoutBOM(t *testing.T) {
	// Fewer than three bytes must not trip the BOM peek.
	_, err := Auths(strings.NewReader("id"), "short.csv")
	if err == nil || !strings.Contains(err.Error(), "missing required column") {
		t.Fatalf("got %v", err)
	}
}
