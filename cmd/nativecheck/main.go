// Command nativecheck validates one clean native executable over owned fixtures.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/blisspixel/nemalo/internal/nativecheck"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

func main() {
	binary := flag.String("binary", "", "explicit locally built Nemalo executable")
	commit := flag.String("commit", "", "expected full source commit")
	output := flag.String("output", "", "new absolute outside-checkout directory; default: fresh system temporary directory")
	terminal := flag.Bool("terminal", false, "also exercise native EPUB/PDF reader and TUI terminal interaction")
	flag.Parse()
	if flag.NArg() != 0 || *binary == "" || *commit == "" {
		fmt.Fprintln(os.Stderr, "use nativecheck --binary FILE --commit FULL_SHA [--output NEW_DIRECTORY] [--terminal]")
		os.Exit(2)
	}
	checkout, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	checkout, err = nativecheck.Checkout(checkout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	result, err := nativecheck.Run(ctx, nativecheck.Request{Binary: *binary, Commit: *commit, Output: *output, Checkout: checkout, Terminal: *terminal})
	if result.Schema != "" {
		if writeErr := textsafe.WriteJSON(os.Stdout, result, true); writeErr != nil {
			fmt.Fprintln(os.Stderr, writeErr)
			os.Exit(1)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
