// Package stagingtest stands in for 7-Zip in tests of packages that read
// archives through staging, so no test runs an installed archiver.
package stagingtest

import (
	"bytes"
	"crypto/sha256"
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
	// Body identifies a private snapshot when InstallMany supplies tree nodes.
	Body []byte
	// Kind is the type switch 7-Zip must be given, such as -tRar5 or -t7z.
	Kind string
	// Header is the archive block 7-Zip prints without -ba, one key per line.
	Header  string
	Members map[string][]byte
	// Headers override the archive block by private snapshot name. Fields add
	// member metadata; Served injects bytes different from the listed members.
	Headers map[string]string
	Fields  map[string]string
	Served  map[string][]byte
	// Attributes supplies exact member type declarations, including directories.
	Attributes map[string]string
	// Volumes, when present, require isolated probes and exact assembled copies.
	Volumes []Volume
}

// Volume describes one private snapshot and its isolated reader response.
type Volume struct {
	Name, Header, Listing string
	Body                  []byte
	Code                  int
}

// Main acts as 7-Zip and exits when the test binary was started as the
// stand-in; call it first from TestMain.
func Main() {
	dir, ok := os.LookupEnv(envDir)
	if !ok {
		return
	}
	args := os.Args[1:]
	if _, err := os.Stat(filepath.Join(dir, "tree-profiles")); err == nil {
		at := slices.Index(args, "--")
		if at < 0 || at+1 >= len(args) {
			os.Exit(7)
		}
		body, err := os.ReadFile(args[at+1])
		profile, readErr := os.ReadFile(filepath.Join(dir, fmt.Sprintf("profile-%x", sha256.Sum256(body))))
		if err != nil || readErr != nil {
			os.Exit(7)
		}
		dir = string(profile)
	}
	read := func(name string) []byte {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return b
	}
	kind := string(read("kind"))
	if len(args) == 0 || !slices.Contains(args, "--") || !slices.Contains(args, kind) || !slices.Contains(args, "-pnemalo-no-password") {
		os.Exit(7)
	}
	switch args[0] {
	case "l":
		archive := args[len(args)-1]
		if sub := filepath.Base(filepath.Dir(archive)); strings.HasPrefix(sub, "volume-") {
			entries, err := os.ReadDir(filepath.Dir(archive))
			body, readErr := os.ReadFile(archive)
			if err != nil || readErr != nil || len(entries) != 1 || string(read(sub+".name")) != filepath.Base(archive) || !bytes.Equal(body, read(sub+".body")) {
				os.Exit(7)
			}
			os.Stdout.WriteString("Listing archive: x\n\n--\n")
			os.Stdout.Write(read(sub + ".header"))
			os.Stdout.WriteString("\n----------\n")
			os.Stdout.Write(read(sub + ".listing"))
			var code int
			fmt.Sscan(string(read(sub+".code")), &code)
			os.Exit(code)
		}
		for _, name := range strings.Fields(string(read("volume-names"))) {
			body, err := os.ReadFile(filepath.Join(filepath.Dir(archive), name))
			if err != nil || !bytes.Equal(body, read("copy-"+name)) {
				os.Exit(7)
			}
		}
		if !slices.Contains(args, "-ba") {
			os.Stdout.WriteString("Listing archive: x\n\n--\n")
			header := "header"
			if _, err := os.Stat(filepath.Join(dir, "header-"+filepath.Base(archive))); err == nil {
				header = "header-" + filepath.Base(archive)
			}
			os.Stdout.Write(read(header))
			os.Stdout.WriteString("\n----------\n")
		}
		os.Stdout.Write(read("listing"))
		os.Exit(0)
	case "e":
		if !slices.Contains(args, "-so") || !slices.Contains(args, "-spd") || !slices.Contains(args, "-ssc") {
			os.Exit(7)
		}
		os.Stdout.Write(read(filepath.Join("members", fmt.Sprintf("%x", args[len(args)-1]))))
		os.Exit(0)
	}
	os.Exit(7)
}

// InstallMany reports only registered snapshot bytes, allowing different
// fixed handlers and member tables in one synthetic archive tree.
func InstallMany(t testing.TB, archives ...Archive) {
	t.Helper()
	dir := t.TempDir()
	for _, a := range archives {
		if a.Kind == "-tzip" && a.Header == "" {
			a.Header = fmt.Sprintf("Type = zip\nPhysical Size = %d\n", len(a.Body))
		} else if (a.Kind == "-tRar" || a.Kind == "-tRar5") && a.Header == "" {
			a.Header = fmt.Sprintf("Type = %s\nPhysical Size = %d\nMultivolume = -\nVolumes = 1\n", strings.TrimPrefix(a.Kind, "-t"), len(a.Body))
		}
		Install(t, a)
		profile := os.Getenv(envDir)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("profile-%x", sha256.Sum256(a.Body))), []byte(profile), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tree-profiles"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envDir, dir)
}

// Install makes staging run this test binary as 7-Zip, reporting a.
func Install(t testing.TB, a Archive) {
	t.Helper()
	// These synchronous fixture children start no application goroutines.
	// Retain race instrumentation/reporting while avoiding the default one
	// second exit sleep on every simulated archiver invocation. The running
	// parent test process already initialized its own race settings.
	t.Setenv("GORACE", strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
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
		attributes := a.Attributes[name]
		if attributes == "" {
			attributes = "A -rw-r--r--"
		}
		fmt.Fprintf(&listing, "Path = %s\nSize = %d\nPacked Size = %d\nModified = 2026-10-19 07:47:40\nAttributes = %s\nCRC = %08X\nEncrypted = -\nMethod = LZMA2:24\nBlock = 0\n%s\n", name, len(body), len(body), attributes, crc32.ChecksumIEEE(body), a.Fields[name])
		if served, ok := a.Served[name]; ok {
			body = served
		}
		if err := os.WriteFile(filepath.Join(dir, "members", fmt.Sprintf("%x", name)), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, header := range a.Headers {
		if err := os.WriteFile(filepath.Join(dir, "header-"+name), []byte(header), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"kind": a.Kind, "header": a.Header, "listing": listing.String()} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var volumeNames []string
	for i, v := range a.Volumes {
		prefix := fmt.Sprintf("volume-%04d", i)
		for name, body := range map[string][]byte{prefix + ".name": []byte(v.Name), prefix + ".body": v.Body, prefix + ".header": []byte(v.Header), prefix + ".listing": []byte(v.Listing), prefix + ".code": fmt.Appendf(nil, "%d", v.Code), "copy-" + v.Name: v.Body} {
			if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		volumeNames = append(volumeNames, v.Name)
	}
	if err := os.WriteFile(filepath.Join(dir, "volume-names"), []byte(strings.Join(volumeNames, "\n")), 0o600); err != nil {
		t.Fatal(err)
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
