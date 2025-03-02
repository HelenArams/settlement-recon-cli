// Command recon reconciles card authorizations against scheme clearing files.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// set by goreleaser / make build
var version = "dev"

// Exit codes. 2 is "the run worked but there is something to look at", so a
// cron job can tell a broken file apart from a bad settlement day.
const (
	exitOK         = 0
	exitError      = 1
	exitExceptions = 2
)

// errExceptions is returned by run when the report contains exceptions. The
// report itself has already been written, so main only needs the exit code.
var errExceptions = errors.New("reconciliation has exceptions")

func main() {
	os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr))
}

func execute(args []string, stdout, stderr io.Writer) int {
	cmd := newRootCmd(stdout, stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errExceptions):
		return exitExceptions
	default:
		fmt.Fprintln(stderr, "error:", err)
		return exitError
	}
}
