package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/HelenArams/settlement-recon-cli/internal/load"
	"github.com/HelenArams/settlement-recon-cli/internal/match"
	"github.com/HelenArams/settlement-recon-cli/internal/model"
	"github.com/HelenArams/settlement-recon-cli/internal/report"
	"github.com/HelenArams/settlement-recon-cli/internal/sample"
)

func newRootCmd(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "recon",
		Short:         "Reconcile card authorizations against scheme clearing records",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(newRunCmd(), newValidateCmd(), newSampleCmd())
	return root
}

type runFlags struct {
	auths, clearing string
	toleranceBps    int
	windowDays      int
	expireDays      int
	asOf            string
	format          string
	out             string
}

func newRunCmd() *cobra.Command {
	var f runFlags
	def := match.DefaultOptions()
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Match clearing records to authorizations and print a report",
		Example: `  recon run --auths auths.csv --clearing clearing.csv
  recon run --auths auths.csv --clearing clearing.csv --tolerance-bps 100 --format json --out report.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRecon(cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&f.auths, "auths", "", "authorizations CSV (required)")
	fl.StringVar(&f.clearing, "clearing", "", "clearing CSV (required)")
	fl.IntVar(&f.toleranceBps, "tolerance-bps", def.ToleranceBps, "amount tolerance for tier-2 matches, in basis points")
	fl.IntVar(&f.windowDays, "window-days", def.WindowDays, "max days between auth and settlement for tier-2/3 matches")
	fl.IntVar(&f.expireDays, "expire-days", def.ExpireDays, "days after which an unmatched auth is reported as expired")
	fl.StringVar(&f.asOf, "as-of", "", "reconciliation date YYYY-MM-DD (default: latest settlement date)")
	fl.StringVar(&f.format, "format", "", "output format: table, json or csv (default: from --out extension, else table)")
	fl.StringVarP(&f.out, "out", "o", "", "write the report to this file instead of stdout")
	_ = cmd.MarkFlagRequired("auths")
	_ = cmd.MarkFlagRequired("clearing")
	return cmd
}

func runRecon(cmd *cobra.Command, f runFlags) error {
	format, err := pickFormat(f.format, f.out)
	if err != nil {
		return err
	}
	opt := match.Options{ToleranceBps: f.toleranceBps, WindowDays: f.windowDays, ExpireDays: f.expireDays}
	if f.asOf != "" {
		opt.AsOf, err = time.Parse("2006-01-02", f.asOf)
		if err != nil {
			return fmt.Errorf("--as-of: want YYYY-MM-DD, got %q", f.asOf)
		}
	}
	if err := opt.Validate(); err != nil {
		return err
	}

	auths, err := load.AuthsFile(f.auths)
	if err != nil {
		return err
	}
	clearings, err := load.ClearingsFile(f.clearing)
	if err != nil {
		return err
	}
	rec, err := match.Reconcile(auths, clearings, opt)
	if err != nil {
		return err
	}

	if f.out == "" {
		if err := report.Write(cmd.OutOrStdout(), rec, format); err != nil {
			return err
		}
	} else {
		if err := writeFile(f.out, func(w io.Writer) error { return report.Write(w, rec, format) }); err != nil {
			return err
		}
		s := report.Summarize(rec)
		fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s: %d results, %d exceptions\n", f.out, s.Total, s.Exceptions)
	}

	if report.Summarize(rec).Exceptions > 0 {
		return errExceptions
	}
	return nil
}

func pickFormat(flag, out string) (report.Format, error) {
	if flag != "" {
		return report.ParseFormat(flag)
	}
	switch strings.ToLower(filepath.Ext(out)) {
	case ".json":
		return report.JSON, nil
	case ".csv":
		return report.CSV, nil
	}
	return report.Table, nil
}

// writeFile writes via a temp file in the same directory and renames it, so a
// failed run never leaves a half-written report where the next job reads it.
func writeFile(path string, fn func(io.Writer) error) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = fn(tmp); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func newValidateCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "validate <file>...",
		Short: "Check that CSV files load cleanly, without reconciling",
		Long: `Validate checks headers and every row of one or more input files and lists
each problem with its line number. The file kind (auths or clearing) is
detected from the header unless --kind is given.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			failed := 0
			for _, path := range args {
				if err := validateFile(cmd.OutOrStdout(), path, load.Kind(kind)); err != nil {
					fmt.Fprintln(cmd.OutOrStdout(), err)
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d file(s) failed validation", failed, len(args))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "force the file kind: auths or clearing")
	return cmd
}

func validateFile(w io.Writer, path string, kind load.Kind) error {
	if kind == "" {
		k, err := load.DetectFile(path)
		if err != nil {
			return err
		}
		kind = k
	}
	var n int
	var err error
	switch kind {
	case load.KindAuths:
		var rows []model.Auth
		rows, err = load.AuthsFile(path)
		n = len(rows)
	case load.KindClearing:
		var rows []model.Clearing
		rows, err = load.ClearingsFile(path)
		n = len(rows)
	default:
		return fmt.Errorf("--kind must be auths or clearing, got %q", kind)
	}
	if err != nil {
		var rowErrs load.Errors
		if errors.As(err, &rowErrs) {
			return fmt.Errorf("%w\n%s: %d bad row(s), %d ok", err, path, len(rowErrs), n)
		}
		return err
	}
	fmt.Fprintf(w, "%s: ok, %d %s record(s)\n", path, n, kind)
	return nil
}

func newSampleCmd() *cobra.Command {
	var dir string
	var force bool
	cmd := &cobra.Command{
		Use:   "sample",
		Short: "Write a sample auths.csv and clearing.csv to play with",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			written, err := sample.Write(dir, force)
			for _, p := range written {
				fmt.Fprintln(cmd.OutOrStdout(), "wrote", p)
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&dir, "dir", "d", ".", "directory to write the files to")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}
