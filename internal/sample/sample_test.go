package sample

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInSyncWithTestdata(t *testing.T) {
	for name, embedded := range map[string][]byte{"auths.csv": Auths, "clearing.csv": Clearing} {
		onDisk, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(onDisk, embedded) {
			t.Errorf("internal/sample/%s differs from testdata/%s", name, name)
		}
	}
}

func TestWriteRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, false); err == nil {
		t.Fatal("second write without force should fail")
	}
	got, err := Write(dir, true)
	if err != nil || len(got) != 2 {
		t.Fatalf("forced write: %v, %v", got, err)
	}
}
