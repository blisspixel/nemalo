// Command collection builds an explicitly selected, untrusted validation intake.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/blisspixel/nemalo/internal/collection"
)

func main() {
	manifest := flag.String("manifest", "collections/foundations.json", "curated manifest path")
	destination := flag.String("destination", "", "explicit intake directory outside the repository")
	reportPath := flag.String("report", "", "new JSON report path; existing reports are preserved")
	indexPath := flag.String("index", "", "optional new Markdown reading-list path")
	flag.Parse()
	if *destination == "" || *reportPath == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "destination and report are required")
		os.Exit(2)
	}
	f, err := os.Open(*manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m, err := collection.Load(f)
	_ = f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := os.OpenFile(*reportPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	report, acquireErr := (collection.Acquirer{Budget: 5 << 30, Interval: 3 * time.Second}).Acquire(ctx, m, *destination, os.Stderr)
	writeErr := json.NewEncoder(out).Encode(report)
	syncErr, closeErr := out.Sync(), out.Close()
	for _, err := range []error{acquireErr, writeErr, syncErr, closeErr} {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if *indexPath != "" {
		prefix, err := filepath.Rel(filepath.Dir(*indexPath), *destination)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		index, err := os.OpenFile(*indexPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		writeErr := collection.WriteIndex(index, m, report, filepath.ToSlash(prefix))
		syncErr, closeErr := index.Sync(), index.Close()
		for _, err := range []error{writeErr, syncErr, closeErr} {
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	}
	fmt.Printf("Acquired %d resources (%d stored bytes). Antivirus has not run.\n", report.AcquiredResources, report.StoredBytes)
}
