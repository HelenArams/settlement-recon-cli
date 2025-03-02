package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recon(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = execute(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// sampleDir writes the bundled samples into a temp dir via `recon sample`.
func sampleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if code, _, stderr := recon(t, "sample", "--dir", dir); code != exitOK {
		t.Fatalf("sample: exit %d: %s", code, stderr)
	}
	return dir
}

func TestRunOnSamplesExitsWithExceptions(t *testing.T) {
	dir := sampleDir(t)
	code, out, _ := recon(t, "run",
		"--auths", filepath.Join(dir, "auths.csv"),
		"--clearing", filepath.Join(dir, "clearing.csv"))
	if code != exitExceptions {
		t.Errorf("exit = %d, want %d", code, exitExceptions)
	}
	for _, want := range []string{"TOTAL                  15  exceptions: 4", "PARTIAL", "DUPLICATE"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
}

func TestRunCleanFileExitsZero(t *testing.T) {
	dir := t.TempDir()
	auths := filepath.Join(dir, "a.csv")
	clearing := filepath.Join(dir, "c.csv")
	write(t, auths, "id,card_token,amount,currency,auth_code,merchant_id,timestamp\nA-1,tok,10.00,EUR,ABC123,M,2025-02-10T10:00:00Z\n")
	write(t, clearing, "arn,card_token,amount,currency,auth_code,merchant_id,settlement_date,type\n1,tok,10.00,EUR,ABC123,M,2025-02-11,presentment\n")

	code, _, stderr := recon(t, "run", "--auths", auths, "--clearing", clearing)
	if code != exitOK {
		t.Errorf("exit = %d, want 0: %s", code, stderr)
	}
}

func TestRunWritesJSONByExtension(t *testing.T) {
	dir := sampleDir(t)
	out := filepath.Join(dir, "report.json")
	code, stdout, stderr := recon(t, "run",
		"--auths", filepath.Join(dir, "auths.csv"),
		"--clearing", filepath.Join(dir, "clearing.csv"),
		"--tolerance-bps", "10", "--out", out)
	if code != exitExceptions {
		t.Fatalf("exit = %d: %s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty with --out, got %q", stdout)
	}
	if !strings.Contains(stderr, "wrote "+out) {
		t.Errorf("stderr = %q", stderr)
	}
	var rep struct {
		Options struct {
			ToleranceBps int `json:"tolerance_bps"`
		} `json:"options"`
		Summary struct {
			Outcomes map[string]int `json:"outcomes"`
		} `json:"summary"`
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	// At 10 bps the 23 bps FX drift no longer fits and drops to PARTIAL.
	if rep.Options.ToleranceBps != 10 || rep.Summary.Outcomes["PARTIAL"] != 2 {
		t.Errorf("got %+v", rep)
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".report.json.*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestRunErrors(t *testing.T) {
	dir := sampleDir(t)
	auths, clearing := filepath.Join(dir, "auths.csv"), filepath.Join(dir, "clearing.csv")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing flag", []string{"run", "--auths", auths}, `required flag(s) "clearing" not set`},
		{"bad format", []string{"run", "--auths", auths, "--clearing", clearing, "--format", "xml"}, "unknown format"},
		{"bad tolerance", []string{"run", "--auths", auths, "--clearing", clearing, "--tolerance-bps", "-5"}, "tolerance must be"},
		{"bad as-of", []string{"run", "--auths", auths, "--clearing", clearing, "--as-of", "14/02/2025"}, "--as-of"},
		{"missing file", []string{"run", "--auths", filepath.Join(dir, "nope.csv"), "--clearing", clearing}, "no such file"},
		{"swapped files", []string{"run", "--auths", clearing, "--clearing", auths}, "missing required column"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := recon(t, tc.args...)
			if code != exitError || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d, stderr %q; want 1 and %q", code, stderr, tc.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	dir := sampleDir(t)
	code, out, _ := recon(t, "validate", filepath.Join(dir, "auths.csv"), filepath.Join(dir, "clearing.csv"))
	if code != exitOK {
		t.Fatalf("exit = %d: %s", code, out)
	}
	if !strings.Contains(out, "ok, 12 auths record(s)") || !strings.Contains(out, "ok, 13 clearing record(s)") {
		t.Errorf("output = %q", out)
	}

	bad := filepath.Join(dir, "bad.csv")
	write(t, bad, "arn,card_token,amount,currency,merchant_id,settlement_date\n1,tok,1.001,EUR,M,2025-02-11\n2,tok,1.00,EUR,M,2025-02-11\n")
	code, out, stderr := recon(t, "validate", bad)
	if code != exitError {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(out, "bad.csv:2: amount") || !strings.Contains(out, "1 bad row(s), 1 ok") {
		t.Errorf("output = %q", out)
	}
	if !strings.Contains(stderr, "1 of 1 file(s) failed validation") {
		t.Errorf("stderr = %q", stderr)
	}

	code, out, _ = recon(t, "validate", "--kind", "auths", filepath.Join(dir, "clearing.csv"))
	if code != exitError || !strings.Contains(out, "missing required column") {
		t.Errorf("forced kind: exit %d, %q", code, out)
	}
}

func TestSampleRefusesOverwrite(t *testing.T) {
	dir := sampleDir(t)
	code, _, stderr := recon(t, "sample", "--dir", dir)
	if code != exitError || !strings.Contains(stderr, "already exists") {
		t.Errorf("exit %d, %q", code, stderr)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
