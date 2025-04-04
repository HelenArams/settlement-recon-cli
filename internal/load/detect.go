package load

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
)

// Kind is which of the two input files a CSV looks like.
type Kind string

const (
	KindAuths    Kind = "auths"
	KindClearing Kind = "clearing"
)

// DetectFile peeks at the header of path. A file with an ARN column is a
// clearing file; anything else that resolves as authorizations is auths.
func DetectFile(path string) (Kind, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return Detect(f, path)
}

func Detect(r io.Reader, name string) (Kind, error) {
	header, err := csv.NewReader(skipBOM(r)).Read()
	if errors.Is(err, io.EOF) {
		return "", fmt.Errorf("%s: file is empty", name)
	}
	if err != nil {
		return "", rowErrorFromCSV(name, err)
	}
	if _, err := resolveLayout(name, header, clearingColumns); err == nil {
		return KindClearing, nil
	}
	l := make(layout)
	for i, cell := range header {
		if normalizeHeader(cell) == "arn" {
			l["arn"] = i
		}
	}
	if l.has("arn") {
		// Looks like clearing but is broken: report it against clearing columns.
		_, err := resolveLayout(name, header, clearingColumns)
		return KindClearing, err
	}
	_, err = resolveLayout(name, header, authColumns)
	return KindAuths, err
}
