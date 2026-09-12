// repair-artist-credits is an offline, report-first managed library credit repair.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/ardam/navidrome-replacement/server/internal/modules/managedimport"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("repair-artist-credits", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	database := flags.String("db", "", "existing SQLite database (required; never migrated)")
	root := flags.String("managed-root", "", "authoritative Managed Storage root (required)")
	apply := flags.Bool("apply", false, "apply repairs; stop the Music Server first")
	backup := flags.String("backup", "", "new SQLite backup path (required with --apply)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*database) == "" || strings.TrimSpace(*root) == "" {
		return errors.New("--db and --managed-root are required; stop the Music Server before running")
	}
	if *apply && strings.TrimSpace(*backup) == "" {
		return errors.New("--apply requires --backup pointing to a new file")
	}
	if !*apply && *backup != "" {
		return errors.New("--backup requires --apply; default mode only reports")
	}
	report, err := managedimport.PreviewArtistCreditRepair(ctx, *database, *root)
	if err != nil {
		return err
	}
	var applyErr error
	if *apply {
		applyErr = report.Apply(ctx, *backup)
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return errors.Join(applyErr, encoder.Encode(report))
}
