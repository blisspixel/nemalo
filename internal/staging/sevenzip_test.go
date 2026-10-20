package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakeSeven answers as 7-Zip from NEMALO_FAKE_7Z: listing.txt for "l", and
// members/HEX(name) for "e", whose switches it requires.
func fakeSeven() {
	dir := os.Getenv("NEMALO_FAKE_7Z")
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
		calls, err := os.OpenFile(filepath.Join(dir, "calls"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(7)
		}
		calls.WriteString("1")
		calls.Close()
		dir = string(profile)
	}
	code := func(name string) int {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0
		}
		var n int
		fmt.Sscan(string(b), &n)
		return n
	}
	kind := os.Getenv("NEMALO_FAKE_7Z_TYPE")
	if b, err := os.ReadFile(filepath.Join(dir, "kind.txt")); err == nil {
		kind = string(b)
	}
	if kind == "" {
		kind = "-tRar5"
	}
	if len(args) == 0 || !slices.Contains(args, noPassword) || !slices.Contains(args, "--") || !slices.Contains(args, kind) {
		os.Exit(7)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "stderr")); err == nil {
		os.Stderr.Write(b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "wait")); len(b) != 0 {
		time.Sleep(30 * time.Second)
	}
	if copies, err := os.ReadFile(filepath.Join(dir, "copies.list")); err == nil {
		archive := args[slices.Index(args, "--")+1]
		for _, name := range strings.Fields(string(copies)) {
			want, readErr := os.ReadFile(filepath.Join(dir, "copy-"+name))
			got, copyErr := os.ReadFile(filepath.Join(filepath.Dir(archive), name))
			if readErr != nil || copyErr != nil || !bytes.Equal(want, got) {
				os.Exit(7)
			}
		}
	}
	switch args[0] {
	case "l":
		listingName := "listing.txt"
		if _, err := os.Stat(filepath.Join(dir, "listing-"+filepath.Base(args[len(args)-1]))); err == nil {
			listingName = "listing-" + filepath.Base(args[len(args)-1])
		}
		if !slices.Contains(args, "-ba") {
			// Without -ba 7-Zip prints the archive's own block first.
			header, exit := "header.txt", "list.code"
			archive := args[len(args)-1]
			if _, err := os.Stat(filepath.Join(dir, "header-"+filepath.Base(archive))); err == nil {
				header = "header-" + filepath.Base(archive)
			}
			if sub := filepath.Base(filepath.Dir(archive)); strings.HasPrefix(sub, "volume-") {
				entries, err := os.ReadDir(filepath.Dir(archive))
				if err != nil || len(entries) != 1 {
					os.Exit(7)
				}
				expected, err := os.ReadFile(filepath.Join(dir, sub+".name"))
				if err != nil || string(expected) != filepath.Base(archive) {
					os.Exit(7)
				}
				header, exit = sub+".header", sub+".code"
			}
			h, _ := os.ReadFile(filepath.Join(dir, header))
			os.Stdout.WriteString("Listing archive: x\n\n--\n")
			os.Stdout.Write(h)
			os.Stdout.WriteString("\n----------\n")
			if exit != "list.code" {
				b, _ := os.ReadFile(filepath.Join(dir, strings.TrimSuffix(header, ".header")+".listing"))
				os.Stdout.Write(b)
				os.Exit(code(exit))
			}
		}
		b, _ := os.ReadFile(filepath.Join(dir, listingName))
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
	// Only synchronous fake-archiver children read this at startup. Race
	// instrumentation and failure reporting remain enabled for every process.
	t.Setenv("GORACE", strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
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

// A RAR set needs compatible isolated-volume evidence and an assembled
// listing that accounts for every volume; a split 7z is joined in order.
func TestVolumeSetsAreReadWholeOrPreserved(t *testing.T) {
	body := []byte("an epub body")
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: body}})
	dir := os.Getenv("NEMALO_FAKE_7Z")
	for _, name := range []string{"set.part1.rar", "set.part2.rar"} {
		data := []byte("Rar!\x1a\x07\x01\x00 volume")
		if err := os.WriteFile(filepath.Join(src.Name(), name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	header := func(text string) {
		if err := os.WriteFile(filepath.Join(dir, "header.txt"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRARProbeFixtures(t, src, []string{"set.part1.rar", "set.part2.rar"}, false, "Rar5")
	header("Path = x\nType = Rar5\nPhysical Size = 15\nTotal Physical Size = 30\nVolume Index = 0\nMultivolume = +\nVolumes = 2\n")
	x, err := ExtractSet(context.Background(), src, []string{"set.part1.rar", "set.part2.rar"}, dst, books, Defaults())
	if err != nil || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
		t.Fatal(x, err)
	}
	header("Path = x\nType = Rar5\nPhysical Size = 15\nTotal Physical Size = 30\nVolume Index = 0\nMultivolume = +\nVolumes = 3\n")
	if _, err := ExtractSet(context.Background(), src, []string{"set.part1.rar", "set.part2.rar"}, dst, books, Defaults()); err == nil || !strings.Contains(err.Error(), "3 volume") {
		t.Fatal(err)
	}
	for name, names := range map[string][]string{
		"gap":       {"set.part1.rar", "set.part3.rar"},
		"other set": {"set.part1.rar", "other.part2.rar"},
		"one":       {"set.part1.rar"},
	} {
		if _, err := ExtractSet(context.Background(), src, names, dst, books, Defaults()); err == nil {
			t.Fatal(name, "accepted")
		}
	}
	// A split 7z is its volumes joined: here a 7z signature across two files.
	t.Setenv("NEMALO_FAKE_7Z_TYPE", "-t7z")
	for name, data := range map[string]string{"set.7z.001": "7z\xbc", "set.7z.002": "\xaf\x27\x1c rest"} {
		if err := os.WriteFile(filepath.Join(src.Name(), name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Remove(filepath.Join(dst.Name(), "00000.epub"))
	header("Path = x\nType = 7z\nPhysical Size = 11\n")
	x, err = ExtractSet(context.Background(), src, []string{"set.7z.001", "set.7z.002"}, dst, books, Defaults())
	if err != nil || len(x.Members) != 1 || x.Members[0].Disposition != "extracted" {
		t.Fatal(x, err)
	}
	if set, n, ok := VolumeOf("dir/Set.Part02.RAR"); !ok || n != 2 || set != filepath.Join("dir", "Set")+".rar" {
		t.Fatal(set, n, ok)
	}
	if _, _, ok := VolumeOf("set.rar"); ok {
		t.Fatal("a plain archive named a volume")
	}
}

// A comment in the archive block cannot speak for 7-Zip, a repeated key
// refuses the block, and a split 7z with bytes past its archive is refused.
func TestArchiveBlockIgnoresCommentsAndExtraVolumes(t *testing.T) {
	block := "Listing archive: x\n\n--\nPath = x\nType = Rar5\nMultivolume = +\nVolumes = 2\nComment = Multivolume = +\nVolumes = 3\n----------\nPath = a\n"
	if keys, ok := archiveBlock([]byte(block)); !ok || keys["Volumes"] != "2" || keys["Multivolume"] != "+" {
		t.Fatal(keys, ok)
	}
	if keys, _ := archiveBlock([]byte("x\n--\nComment = hi\nMultivolume = +\nVolumes = 3\n")); keys["Volumes"] != "" || keys["Multivolume"] != "" {
		t.Fatal("values after a comment were read", keys)
	}
	if _, ok := archiveBlock([]byte("x\n--\nVolumes = 2\nVolumes = 3\n")); ok {
		t.Fatal("a repeated key was accepted")
	}
	src, dst := sevenFixture(t, []sevenMember{{name: "book.epub", body: []byte("x")}})
	t.Setenv("NEMALO_FAKE_7Z_TYPE", "-t7z")
	dir := os.Getenv("NEMALO_FAKE_7Z")
	for name, data := range map[string]string{"s.7z.001": "7z\xbc", "s.7z.002": "\xaf\x27\x1c rest", "s.7z.003": "stray"} {
		if err := os.WriteFile(filepath.Join(src.Name(), name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 7-Zip reads an 11-byte archive; the three volumes join to 16 bytes.
	if err := os.WriteFile(filepath.Join(dir, "header.txt"), []byte("Path = x\nType = 7z\nPhysical Size = 11\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractSet(context.Background(), src, []string{"s.7z.001", "s.7z.002", "s.7z.003"}, dst, books, Defaults()); err == nil || !strings.Contains(err.Error(), "exactly one 7z") {
		t.Fatal(err)
	}
}

// An archive is whole only when its block, as 7-Zip 26 prints it, has no
// comment, warning, offset, or tail, and the named size equals the bytes read.
func TestWholeBlockNeedsNothingBeyondTheMembers(t *testing.T) {
	clean := "Scanning\n\n--\nPath = one.7z\nType = 7z\nPhysical Size = 123\nHeaders Size = 114\nSolid = -\nBlocks = 1\n\n----------\nPath = a.txt\n"
	if !wholeBlock([]byte(clean), "Physical Size", 123) || wholeBlock([]byte(clean), "Physical Size", 127) {
		t.Fatal("size")
	}
	set := "x\n--\nPath = set.part1.rar\nType = Rar5\nPhysical Size = 138595\nTotal Physical Size = 361184\nMultivolume = +\nVolumes = 2\n\n----------\n"
	if !wholeBlock([]byte(set), "Total Physical Size", 361184) {
		t.Fatal("set")
	}
	for name, block := range map[string]string{
		"tail":      "x\n--\nPath = one.7z\nType = 7z\nWARNINGS:\nThere are data after the end of archive\nPhysical Size = 123\nTail Size = 4\n\n----------\n",
		"comment":   "x\n--\nPath = c.rar\nType = Rar5\nComment = Physical Size = 123\nPhysical Size = 123\n\n----------\n",
		"offset":    "x\n--\nPath = s.exe\nType = Rar5\nOffset = 4096\nPhysical Size = 123\n\n----------\n",
		"repeated":  "x\n--\nPhysical Size = 1\nPhysical Size = 123\n\n----------\n",
		"no end":    "x\n--\nPhysical Size = 123\n",
		"no block":  "Physical Size = 123\n----------\n",
		"other key": "x\n--\nPath = one.7z\nPhysical Size = 99\n\n----------\n",
	} {
		if wholeBlock([]byte(block), "Physical Size", 123) {
			t.Error(name, "was whole")
		}
	}
}

// A member 7-Zip marks as a folder but that holds data is not a directory.
func TestAFolderWithDataIsNotADirectory(t *testing.T) {
	for _, e := range []sevenEntry{{name: "hidden", folder: true, size: 1269}, {name: "hidden", folder: true, packed: 9}} {
		if d, _ := classifySeven(e, books, Defaults()); d != "directory_with_data" {
			t.Fatal(d)
		}
	}
	if d, _ := classifySeven(sevenEntry{name: "dir", folder: true}, books, Defaults()); d != "directory" {
		t.Fatal(d)
	}
}
