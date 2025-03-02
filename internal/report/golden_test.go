package report

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/HelenArams/settlement-recon-cli/internal/load"
	"github.com/HelenArams/settlement-recon-cli/internal/match"
)

// go test ./internal/report -run Golden -update
var update = flag.Bool("update", false, "rewrite golden files")

func TestGolden(t *testing.T) {
	auths, err := load.AuthsFile("../../testdata/auths.csv")
	if err != nil {
		t.Fatal(err)
	}
	clearings, err := load.ClearingsFile("../../testdata/clearing.csv")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := match.Reconcile(auths, clearings, match.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range Formats {
		t.Run(string(f), func(t *testing.T) {
			var buf bytes.Buffer
			if err := Write(&buf, rec, f); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "sample."+string(f)+".golden")
			if *update {
				if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to create)", err)
			}
			if !bytes.Equal(buf.Bytes(), want) {
				t.Errorf("output differs from %s; rerun with -update if the change is intended\n--- got:\n%s", path, buf.String())
			}
		})
	}
}
