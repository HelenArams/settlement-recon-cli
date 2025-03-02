// Package sample ships a small pair of input files that exercise every
// matching tier and outcome. `recon sample` writes them out so new users have
// something to run against.
package sample

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// NOTE: these are copies of /testdata; TestInSyncWithTestdata keeps them honest.
var (
	//go:embed auths.csv
	Auths []byte
	//go:embed clearing.csv
	Clearing []byte
)

// Write puts auths.csv and clearing.csv into dir. It refuses to overwrite
// existing files unless force is set.
func Write(dir string, force bool) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	files := []struct {
		name string
		data []byte
	}{{"auths.csv", Auths}, {"clearing.csv", Clearing}}

	var written []string
	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				return written, fmt.Errorf("%s already exists (use --force to overwrite)", path)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return written, err
			}
		}
		if err := os.WriteFile(path, f.data, 0o644); err != nil {
			return written, err
		}
		written = append(written, path)
	}
	return written, nil
}
