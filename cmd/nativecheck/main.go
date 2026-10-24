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
	installed := flag.Bool("installed-tools", false, "also exercise the installed tools named below (decision 0093)")
	var tools nativecheck.InstalledTools
	flag.StringVar(&tools.ClamScan, "clamscan", "", "absolute path of the installed clamscan (Linux and macOS)")
	flag.StringVar(&tools.Defender, "defender", "", "absolute path of the installed MpCmdRun.exe (Windows)")
	flag.StringVar(&tools.SevenZip, "sevenzip", "", "absolute path of the installed 7zz, 7z or 7za")
	flag.StringVar(&tools.FFmpeg, "ffmpeg", "", "absolute path of the installed ffmpeg")
	flag.StringVar(&tools.FFprobe, "ffprobe", "", "absolute path of the installed ffprobe")
	flag.Parse()
	named := tools != nativecheck.InstalledTools{}
	if flag.NArg() != 0 || *binary == "" || *commit == "" || named != *installed {
		fmt.Fprintln(os.Stderr, "use nativecheck --binary FILE --commit FULL_SHA [--output NEW_DIRECTORY] [--terminal] [--installed-tools --sevenzip FILE --ffmpeg FILE --ffprobe FILE (--clamscan FILE | --defender FILE)]")
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
	request := nativecheck.Request{Binary: *binary, Commit: *commit, Output: *output, Checkout: checkout, Terminal: *terminal}
	if *installed {
		request.Installed = &tools
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	result, err := nativecheck.Run(ctx, request)
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
