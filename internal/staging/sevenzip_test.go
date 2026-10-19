package staging

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeSeven answers as 7-Zip from NEMALO_FAKE_7Z: listing.txt for "l", and
// members/HEX(name) for "e", whose switches it requires.
func fakeSeven() {
	dir := os.Getenv("NEMALO_FAKE_7Z")
	args := os.Args[1:]
	code := func(name string) int {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0
		}
		var n int
		fmt.Sscan(string(b), &n)
		return n
	}
	if len(args) == 0 || !slices.Contains(args, noPassword) || !slices.Contains(args, "--") || !slices.Contains(args, "-tRar5") {
		os.Exit(7)
	}
	switch args[0] {
	case "l":
		b, _ := os.ReadFile(filepath.Join(dir, "listing.txt"))
		os.Stdout.Write(b)
		os.Exit(code("list.code"))
	case "e":
		if !slices.Contains(args, "-so") || !slices.Contains(args, "-spd") || !slices.Contains(args, "-ssc") {
			os.Exit(7)
		}
		name := args[len(args)-1]
		b, _ := os.ReadFile(filepath.Join(dir, "members", hex.EncodeToString([]byte(name))))
		os.Stdout.Write(b)
		os.Exit(code("extract.code"))
	}
	os.Exit(7)
}

func TestMain(m *testing.M) {
	if _, ok := os.LookupEnv("NEMALO_FAKE_7Z"); ok {
		fakeSeven()
	}
	os.Exit(m.Run())
}

type sevenMember struct {
	name, attributes string
	extra            string
	body             []byte
	listedSize       int64
	listedCRC        string
	packed           string
	encrypted        bool
	served           []byte
}

// sevenFixture writes a fake 7-Zip state and an archive file to read.
func sevenFixture(t *testing.T, members []sevenMember) (*os.Root, *os.Root) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "members"), 0o700); err != nil {
		t.Fatal(err)
	}
	var listing strings.Builder
	for _, m := range members {
		size, crc := int64(len(m.body)), fmt.Sprintf("%08X", crc32.ChecksumIEEE(m.body))
		if m.listedSize != 0 {
			size = m.listedSize
		}
		if m.listedCRC == "-" {
			crc = ""
		} else if m.listedCRC != "" {
			crc = m.listedCRC
		}
		attributes := firstNonEmptyString(m.attributes, "A -rw-r--r--")
		encrypted := "-"
		if m.encrypted {
			encrypted = "+"
		}
		fmt.Fprintf(&listing, "Path = %s\nSize = %d\nPacked Size = %s\nModified = 2026-10-19 07:47:40\nAttributes = %s\nCRC = %s\nEncrypted = %s\nMethod = LZMA2:24\nBlock = 0\n%s\n", m.name, size, m.packed, attributes, crc, encrypted, m.extra)
		served := m.body
		if m.served != nil {
			served = m.served
		}
		if err := os.WriteFile(filepath.Join(dir, "members", hex.EncodeToString([]byte(m.name))), served, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "listing.txt"), []byte(listing.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	saved := SevenZip
	SevenZip = func() (string, error) { return exe, nil }
	t.Cleanup(func() { SevenZip = saved })
	t.Setenv("NEMALO_FAKE_7Z", dir)
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "bundle.rar"), []byte("Rar!\x1a\x07\x01\x00 fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := os.OpenRoot(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close(); dst.Close() })
	return src, dst
}

func firstNonEmptyString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func books(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".epub") || strings.HasSuffix(strings.ToLower(name), ".m4b")
}

// Every member of a RAR or 7z listing is accounted for; only eligible members
// are extracted, by exact name, and each must have its listed size and CRC.
func TestSevenZipMembersAreClassifiedAndChecked(t *testing.T) {
	good := []byte("an epub body")
	src, dst := sevenFixture(t, []sevenMember{
		{name: "books", attributes: "D drwxr-xr-x"},
		{name: "books/a*b?.epub", body: good},
		{name: "notes.txt", body: []byte("notes")},
		{name: "link.epub", body: []byte("/etc/passwd"), attributes: "A lrwxrwxrwx"},
		{name: "locked.epub", body: good, encrypted: true},
		{name: "../escape.epub", body: good},
		{name: "twice.epub", body: good},
		{name: "twice.epub", body: good},
		{name: "inner.zip", body: good},
		{name: "badcrc.epub", body: good, listedCRC: "DEADBEEF"},
		{name: "longer.epub", body: good, served: append(append([]byte{}, good...), 'x')},
		{name: "missing.epub", body: good, served: []byte{}},
		{name: "bomb.epub", body: []byte(strings.Repeat("z", 5000)), packed: "1"},
		{name: "huge.m4b", body: good, listedSize: 300 << 20},
		// RAR states only a Unix mode, after empty attribute letters.
		{name: "rarlink.epub", body: []byte("target"), attributes: " lrwxrwxrwx"},
		{name: "rardir", attributes: " drwxr-xr-x"},
		{name: "fifo.epub", body: good, attributes: " prw-r--r--"},
		// Review regressions: a link stated by Mode or a link key, a hex
		// attribute word holding D, a name that is also a folder, no CRC.
		{name: "modelink.epub", body: []byte("/etc/passwd"), attributes: "A", extra: "Mode = lrwxrwxrwx\n"},
		{name: "rar5link.epub", body: []byte("target"), attributes: "A", extra: "Symbolic Link = /etc/passwd\n"},
		{name: "hexattr.epub", body: good, attributes: "V 0D800000 0rwS--S---"},
		{name: "shadow.epub", body: good},
		{name: "shadow.epub/inner.epub", body: good},
		{name: "nocrc.epub", body: good, listedCRC: "-"},
	})
	x, err := Extract(context.Background(), src, "bundle.rar", dst, books, Defaults())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"books": "directory", "books/a*b?.epub": "extracted", "notes.txt": "unsupported_format", "link.epub": "link",
		"locked.epub": "encrypted", "../escape.epub": "unsafe_name", "twice.epub": "duplicate_name", "inner.zip": "nested_archive",
		"badcrc.epub": "failed", "longer.epub": "failed", "missing.epub": "failed", "bomb.epub": "suspicious_ratio", "huge.m4b": "too_large",
		"rarlink.epub": "link", "rardir": "directory", "fifo.epub": "special",
		"modelink.epub": "link", "rar5link.epub": "link", "hexattr.epub": "extracted", "shadow.epub": "duplicate_name",
		"shadow.epub/inner.epub": "extracted", "nocrc.epub": "unsupported_method",
	}
	if len(x.Members) != 23 || x.Complete {
		t.Fatal(len(x.Members), x.Complete)
	}
	for _, m := range x.Members {
		if want[m.Name] != m.Disposition {
			t.Errorf("%s: %s (%s), want %s", m.Name, m.Disposition, m.Detail, want[m.Name])
		}
	}
	staged, _ := os.ReadDir(dst.Name())
	if len(staged) != 3 || staged[0].Name() != "00001.epub" {
		t.Fatal("staged", staged)
	}
	if b, _ := os.ReadFile(filepath.Join(dst.Name(), "00001.epub")); string(b) != string(good) {
		t.Fatal("staged bytes differ")
	}
}

func TestSevenZipRefusesWhatItCannotList(t *testing.T) {
	src, dst := sevenFixture(t, []sevenMember{{name: "a.epub", body: []byte("x")}})
	dir := os.Getenv("NEMALO_FAKE_7Z")
	// A listing line that is not "Key = Value", as a name with a line break
	// would produce, refuses the whole archive.
	if err := os.WriteFile(filepath.Join(dir, "listing.txt"), []byte("Path = a\nb.epub\nSize = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract7z(context.Background(), src, "bundle.rar", dst, books, Defaults()); err == nil || !strings.Contains(err.Error(), "one line") {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "list.code"), []byte("2"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Extract7z(context.Background(), src, "bundle.rar", dst, books, Defaults()); err == nil || !strings.Contains(err.Error(), "could not list") {
		t.Fatal(err)
	}
	// A file whose signature is not the extension's archive is never read.
	for name, body := range map[string]string{"tar.rar": "ustar\x00 not rar", "zip.7z": "PK\x03\x04"} {
		if err := os.WriteFile(filepath.Join(src.Name(), name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Extract7z(context.Background(), src, name, dst, books, Defaults()); err == nil || !strings.Contains(err.Error(), "signature") {
			t.Fatal(name, err)
		}
	}
	SevenZip = func() (string, error) { return "", errors.New("missing") }
	if _, err := Extract(context.Background(), src, "bundle.rar", dst, books, Defaults()); !errors.Is(err, ErrNoArchiveTool) {
		t.Fatal(err)
	}
	if !Supported("x.7z") || !Supported("x.RAR") || Supported("x.tar") {
		t.Fatal("supported archives")
	}
}
