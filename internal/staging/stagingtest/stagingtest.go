// Package stagingtest stands in for 7-Zip in tests of packages that read
// archives through staging, so no test runs an installed archiver.
package stagingtest

import (
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging"
)

const envDir = "NEMALO_STAGINGTEST_7Z"

// Archive is what the stand-in reports for any archive it is asked about.
type Archive struct {
	// Kind is the type switch 7-Zip must be given, such as -tRar5 or -t7z.
	Kind string
	// Header is the archive block 7-Zip prints without -ba, one key per line.
	Header  string
	Members map[string][]byte
}

// Main acts as 7-Zip and exits when the test binary was started as the
// stand-in; call it first from TestMain.
func Main() {
	dir, ok := os.LookupEnv(envDir)
	if !ok {
		return
	}
	read := func(name string) []byte {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return b
	}
	args := os.Args[1:]
	kind := string(read("kind"))
	if len(args) == 0 || !slices.Contains(args, "--") || !slices.Contains(args, kind) || !slices.Contains(args, "-pnemalo-no-password") {
		os.Exit(7)
	}
	switch args[0] {
	case "l":
		if !slices.Contains(args, "-ba") {
			os.Stdout.WriteString("Listing archive: x\n\n--\n")
			os.Stdout.Write(read("header"))
			os.Stdout.WriteString("\n----------\n")
		}
		os.Stdout.Write(read("listing"))
		os.Exit(0)
	case "e":
		if !slices.Contains(args, "-so") {
			os.Exit(7)
		}
		os.Stdout.Write(read(filepath.Join("members", fmt.Sprintf("%x", args[len(args)-1]))))
		os.Exit(0)
	}
	os.Exit(7)
}

// Install makes staging run this test binary as 7-Zip, reporting a.
func Install(t testing.TB, a Archive) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "members"), 0o700); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(a.Members))
	for name := range a.Members {
		names = append(names, name)
	}
	sort.Strings(names)
	var listing strings.Builder
	for _, name := range names {
		body := a.Members[name]
		fmt.Fprintf(&listing, "Path = %s\nSize = %d\nPacked Size = %d\nModified = 2026-10-19 07:47:40\nAttributes = A -rw-r--r--\nCRC = %08X\nEncrypted = -\nMethod = LZMA2:24\nBlock = 0\n\n", name, len(body), len(body), crc32.ChecksumIEEE(body))
		if err := os.WriteFile(filepath.Join(dir, "members", fmt.Sprintf("%x", name)), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"kind": a.Kind, "header": a.Header, "listing": listing.String()} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	saved := staging.SevenZip
	staging.SevenZip = func() (string, error) { return exe, nil }
	t.Cleanup(func() { staging.SevenZip = saved })
	t.Setenv(envDir, dir)
}
