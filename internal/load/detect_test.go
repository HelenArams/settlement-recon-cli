package load

import (
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		header  string
		want    Kind
		wantErr bool
	}{
		{"id,card_token,amount,currency,merchant_id,timestamp", KindAuths, false},
		{"\xEF\xBB\xBFarn,card_token,amount,currency,merchant_id,settlement_date", KindClearing, false},
		{"ARN,token,amount,ccy", KindClearing, true},
		{"foo,bar", KindAuths, true},
	}
	for _, tc := range tests {
		got, err := Detect(strings.NewReader(tc.header+"\n"), "x.csv")
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("Detect(%q) = %q, %v", tc.header, got, err)
		}
	}
	if _, err := Detect(strings.NewReader(""), "x.csv"); err == nil {
		t.Error("empty file accepted")
	}
}
