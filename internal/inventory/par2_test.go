package inventory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/inventory/inventorytest"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

func writePAR2Files(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func par2Limits() Limits {
	l := Defaults()
	l.PAR2 = true
	return l
}

func entryPAR2(t *testing.T, r Report, path string) *PAR2File {
	t.Helper()
	for _, entry := range r.Entries {
		if entry.Path == path && entry.PAR2 != nil {
			return entry.PAR2
		}
	}
	t.Fatal("missing PAR2 evidence", path, r)
	return nil
}

func TestPAR2InventoryRequiresOptInAndGroupsDeclarations(t *testing.T) {
	dir := t.TempDir()
	a := inventorytest.File(inventorytest.Main(1, 4096, 1, 10), inventorytest.Description(1, 10, []byte("books/book.epub"), 1234), inventorytest.Creator(1, "Synthetic fixture"), inventorytest.Packet(1, "PAR 2.0\x00RecvSlic", make([]byte, 32)), inventorytest.Packet(1, "PAR 2.0\x00IFSC", make([]byte, 36)))
	b := inventorytest.File(inventorytest.Main(2, 8192, 1, 20), inventorytest.Creator(2, "Independent set"))
	files := map[string][]byte{"same.par2": a, "renamed.bin": a, "same.vol00-01.par2": b, "false.par2": []byte("not a PAR2 packet"), "note.txt": []byte("ordinary input")}
	writePAR2Files(t, dir, files)
	plain, err := Inspect(context.Background(), dir, Defaults())
	if err != nil || plain.BytesRead != 0 || plain.PAR2 != nil {
		t.Fatal("default inventory read bodies", plain, err)
	}
	for _, entry := range plain.Entries {
		if entry.PAR2 != nil || entry.SHA256 != "" {
			t.Fatal("metadata without opt-in", entry)
		}
	}
	r, err := Inspect(context.Background(), dir, par2Limits())
	if err != nil || !r.Complete || r.Security != "not_scanned" || r.PAR2 == nil || !r.PAR2.Complete || len(r.PAR2.Sets) != 2 {
		t.Fatal(r, err)
	}
	if r.PAR2.Scope != "packet_metadata_only" || r.PAR2.ProtectedData != "not_verified" || r.PAR2.Recoverability != "not_assessed" || r.BytesRead != r.PAR2.BytesRead || r.PAR2.FilesProbed != len(files) {
		t.Fatal(r.PAR2)
	}
	set := r.PAR2.Sets[0]
	if set.ID != strings.Repeat("01", 16) || len(set.Files) != 2 || len(set.Mains) != 1 || len(set.Mains[0].Locations) != 2 || len(set.Descriptions) != 1 || len(set.Descriptions[0].Locations) != 2 || set.Descriptions[0].Name != "books/book.epub" || set.Descriptions[0].Bytes != 1234 || set.Unchecked != 4 {
		t.Fatal(set)
	}
	if m := entryPAR2(t, r, "renamed.bin"); !m.HeadersComplete || m.Matching != 3 || m.Unchecked != 2 || m.ParityHeaders != 1 || set.ParityHeaders != 2 || m.InputHeaders != 1 || set.InputHeaders != 2 {
		t.Fatal(m)
	}
	if m := entryPAR2(t, r, "false.par2"); m.State != "signature_not_matched" || !m.ProbeComplete || m.HeadersComplete || len(m.SetIDs) != 0 {
		t.Fatal(m)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("source changed", name, err)
		}
	}
	// SHA-256 remains optional and binds the whole file, not a packet or set ID.
	l := par2Limits()
	l.Hashes = true
	r, err = Inspect(context.Background(), dir, l)
	if err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	var expected int64
	for _, entry := range r.Entries {
		sum := sha256.Sum256(files[entry.Path])
		if entry.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal(entry)
		}
		expected += int64(len(files[entry.Path]))
	}
	if r.BytesRead != r.PAR2.BytesRead+expected {
		t.Fatal("metadata or hashing escaped actual read accounting", r.BytesRead, r.PAR2.BytesRead, expected)
	}
}

func TestPAR2ConflictsAndChecksumEvidenceStaySeparate(t *testing.T) {
	dir := t.TempDir()
	bad := inventorytest.Main(3, 16384, 1, 30)
	bad[64] ^= 1
	body := inventorytest.File(inventorytest.Main(3, 4096, 1, 30), inventorytest.Main(3, 4096, 1, 30), inventorytest.Main(3, 8192, 1, 30),
		inventorytest.Description(3, 30, []byte("same.epub"), 100), inventorytest.Description(3, 30, []byte("same.epub"), 200),
		inventorytest.Description(3, 31, []byte("same.epub"), 100), bad, inventorytest.Packet(3, "Unknown format", []byte("opaque bytes")))
	writePAR2Files(t, dir, map[string][]byte{"conflicts.par2": body})
	r, err := Inspect(context.Background(), dir, par2Limits())
	if err != nil || !r.Complete || !r.PAR2.Complete || len(r.PAR2.Sets) != 1 {
		t.Fatal(r, err)
	}
	set := r.PAR2.Sets[0]
	if len(set.Mains) != 2 || len(set.Mains[0].Locations) != 2 || len(set.Descriptions) != 3 || set.Mismatched != 1 || set.Unchecked != 1 {
		t.Fatal("chose a conflicting winner or trusted corrupt metadata", set)
	}
	for _, want := range []string{"conflicting_main_packets", "conflicting_file_descriptions", "filename_declared_for_multiple_file_ids"} {
		if !containsString(set.Conflicts, want) {
			t.Fatal(want, set.Conflicts)
		}
	}
	m := entryPAR2(t, r, "conflicts.par2")
	if !m.HeadersComplete || m.Mismatched != 1 || m.Unchecked != 1 || m.UnknownHeaders != 1 || set.UnknownHeaders != 1 || len(m.Findings) != 2 {
		t.Fatal(m)
	}
}

func TestPAR2DeclaredFilenamesNeverAuthorizeOtherReads(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "canary.txt")
	canary := []byte("OUTSIDE-CONTENT-NOT-IN-METADATA")
	if err := os.WriteFile(outside, canary, 0o600); err != nil {
		t.Fatal(err)
	}
	names := [][]byte{[]byte(outside), []byte("../../outside.epub"), []byte(`C:\Windows\file.epub`), []byte("https://example.invalid/upload"), []byte("\x1b[31m\u202ebook.epub"), {0xff, 0xfe}}
	var packets [][]byte
	for i, name := range names {
		packets = append(packets, inventorytest.Description(4, byte(i), name, ^uint64(0)))
	}
	packets = append(packets, inventorytest.Creator(4, "\x1b[2Jprovider instruction https://example.invalid"))
	body := inventorytest.File(packets...)
	writePAR2Files(t, dir, map[string][]byte{"names.par2": body})
	r, err := Inspect(context.Background(), dir, par2Limits())
	if err != nil || len(r.Entries) != 1 || len(r.PAR2.Sets) != 1 || len(r.PAR2.Sets[0].Descriptions) != len(names) {
		t.Fatal(r, err)
	}
	for i, d := range r.PAR2.Sets[0].Descriptions {
		if d.Bytes != ^uint64(0) || i < len(names)-1 && d.Name != string(names[i]) {
			t.Fatal(d)
		}
	}
	opaque := r.PAR2.Sets[0].Descriptions[len(names)-1]
	if opaque.Name != "" || opaque.NameHex != "fffe" {
		t.Fatal("opaque filename bytes were guessed", opaque)
	}
	encoded, err := textsafe.MarshalJSON(r, false)
	if err != nil || bytes.Contains(encoded, canary) || bytes.ContainsRune(encoded, '\x1b') || bytes.ContainsRune(encoded, '\u202e') || !bytes.Contains(encoded, []byte(`\u202e`)) {
		t.Fatal(string(encoded), err)
	}
	if got, _ := os.ReadFile(outside); !bytes.Equal(got, canary) || r.BytesRead > int64(len(body)) {
		t.Fatal("declared filename caused I/O outside the metadata", r.BytesRead)
	}
}

func TestPAR2MalformedHeadersRemainIncomplete(t *testing.T) {
	valid := inventorytest.Creator(5, "fixture")
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"signature", []byte("PAR2\x00P")}, {"header", valid[:60]},
		{"zero length", mutatePAR2Length(valid, 0)}, {"short length", mutatePAR2Length(valid, 60)},
		{"alignment", mutatePAR2Length(valid, 65)}, {"overflow", mutatePAR2Length(valid, ^uint64(0))},
		{"truncated body", mutatePAR2Length(valid, uint64(len(valid)+4))},
		{"junk between packets", inventorytest.File(valid, []byte("garbage after packet"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writePAR2Files(t, dir, map[string][]byte{"bad.par2": tc.body})
			r, err := Inspect(context.Background(), dir, par2Limits())
			if err == nil || r.Complete || r.PAR2.Complete || entryPAR2(t, r, "bad.par2").HeadersComplete || r.BytesRead > int64(len(tc.body)) {
				t.Fatal(r, err)
			}
		})
	}
}

func mutatePAR2Length(body []byte, length uint64) []byte {
	b := append([]byte{}, body...)
	binary.LittleEndian.PutUint64(b[8:], length)
	return b
}

func TestPAR2ReadBudgetsApplyWithoutHashesAndAcrossFiles(t *testing.T) {
	body := inventorytest.File(inventorytest.Main(6, 4096, 1, 1), inventorytest.Creator(6, "metadata"))
	for _, hashes := range []bool{false, true} {
		for _, fileLimit := range []bool{false, true} {
			dir := t.TempDir()
			writePAR2Files(t, dir, map[string][]byte{"a.par2": body, "b.par2": body, "c.par2": body})
			l := par2Limits()
			l.Hashes = hashes
			if fileLimit {
				l.FileBytes = 80
			} else {
				l.TotalBytes = int64(len(body)) + 70
			}
			r, err := Inspect(context.Background(), dir, l)
			if err == nil || r.Complete || r.PAR2.Complete || r.BytesRead > l.TotalBytes || r.PAR2.BytesRead > r.BytesRead || len(r.Entries) != 3 {
				t.Fatal(r, err)
			}
			for _, entry := range r.Entries {
				if fileLimit && (entry.Finding == "" || entry.SHA256 != "") {
					t.Fatal(entry)
				}
			}
		}
	}
}

func TestPAR2PacketAndSetLimitsAreSharedAcrossFiles(t *testing.T) {
	t.Run("packets", func(t *testing.T) {
		dir := t.TempDir()
		packet := inventorytest.Creator(7, "test")
		files := map[string][]byte{}
		for i := 0; i < 18; i++ {
			files[fmt.Sprintf("%02d.par2", i)] = bytes.Repeat(packet, par2FilePackets)
		}
		writePAR2Files(t, dir, files)
		r, err := Inspect(context.Background(), dir, par2Limits())
		if err == nil || r.Complete || r.PAR2.Packets != par2Packets || r.PAR2.FilesProbed != 17 || len(r.Entries) != len(files) || r.BytesRead != int64(par2Packets*len(packet)+8) {
			t.Fatal(r, err)
		}
		if last := entryPAR2(t, r, "17.par2"); last.State != "not_inspected" || last.Packets != 0 {
			t.Fatal(last)
		}
	})
	t.Run("sets", func(t *testing.T) {
		dir := t.TempDir()
		files := map[string][]byte{}
		for i := 0; i < par2Sets+2; i++ {
			files[fmt.Sprintf("%03d.par2", i)] = inventorytest.Creator(byte(i), "one")
		}
		writePAR2Files(t, dir, files)
		r, err := Inspect(context.Background(), dir, par2Limits())
		if err == nil || r.Complete || len(r.PAR2.Sets) != par2Sets || r.PAR2.FilesProbed != par2Sets+1 || len(r.Entries) != len(files) {
			t.Fatal(r, err)
		}
	})
	// A file limit does not reset the run counter or suppress later entries.
	dir := t.TempDir()
	writePAR2Files(t, dir, map[string][]byte{"a.par2": bytes.Repeat(inventorytest.Creator(8, "one"), par2FilePackets+1), "b.par2": inventorytest.Creator(8, "two")})
	r, err := Inspect(context.Background(), dir, par2Limits())
	if err == nil || r.Complete || r.PAR2.Packets != par2FilePackets+1 || !entryPAR2(t, r, "b.par2").HeadersComplete {
		t.Fatal(r, err)
	}
}

type sparsePAR2 struct {
	size int64
	last []byte
	head []byte
}

func (r sparsePAR2) ReadAt(p []byte, off int64) (int, error) {
	var part []byte
	if off >= 0 && off+int64(len(p)) <= int64(len(r.head)) {
		part = r.head[off:]
	} else if start := r.size - int64(len(r.last)); off >= start && off+int64(len(p)) <= r.size {
		part = r.last[off-start:]
	} else {
		return 0, errors.New("attempted to read parity payload")
	}
	return copy(p, part), nil
}

func TestPAR2LargeParityExtentNeverReadsPayload(t *testing.T) {
	const extent = 1 << 40
	head := inventorytest.Packet(9, "PAR 2.0\x00RecvSlic", nil)
	binary.LittleEndian.PutUint64(head[8:], extent)
	tail := inventorytest.Creator(9, "metadata after parity")
	data := sparsePAR2{size: extent + int64(len(tail)), head: head, last: tail}
	run := newPAR2Run()
	var report Report
	r := &fileReader{ctx: context.Background(), f: data, limits: par2Limits(), report: &report, par2: run}
	m, observations, err := inspectPAR2(context.Background(), r, data.size, true)
	if err != nil || !m.HeadersComplete || m.Unchecked != 1 || m.Matching != 1 || len(observations) != 2 || report.BytesRead != int64(len(head)+len(tail)) {
		t.Fatal(m, err, report.BytesRead)
	}
}

type mutateInventoryFile struct {
	*os.File
	onRead func()
}

func (f *mutateInventoryFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.File.ReadAt(p, off)
	if f.onRead != nil {
		fn := f.onRead
		f.onRead = nil
		fn()
	}
	return n, err
}

func TestPAR2ChangedSourceDiscardsHashAndDeclarations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "changing.par2")
	body := inventorytest.Creator(10, "changing source")
	writePAR2Files(t, dir, map[string][]byte{"changing.par2": body})
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &mutateInventoryFile{File: f, onRead: func() {
		if err := os.WriteFile(path, append(body, 0), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	l := par2Limits()
	l.Hashes = true
	run := newPAR2Run()
	var report Report
	sum, metadata, observations, err := readOpened(context.Background(), wrapped, before, "changing.par2", l, &report, run)
	if err == nil || sum != "" || metadata == nil || metadata.State != "source_changed" || len(metadata.SetIDs) != 0 || len(observations) != 0 {
		t.Fatal(sum, metadata, observations, err)
	}
}

func TestPAR2CancellationAndLinksRemainBounded(t *testing.T) {
	dir := t.TempDir()
	writePAR2Files(t, dir, map[string][]byte{"one.par2": inventorytest.Creator(11, "one")})
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	r, err := Inspect(ctx, dir, par2Limits())
	if !errors.Is(err, context.DeadlineExceeded) || r.Complete || r.BytesRead != 0 {
		t.Fatal(r, err)
	}
	link := filepath.Join(dir, "link.par2")
	if err := os.Symlink(filepath.Join(dir, "one.par2"), link); err == nil {
		r, err := Inspect(context.Background(), dir, par2Limits())
		if err != nil || r.PAR2.FilesProbed != 1 {
			t.Fatal(r, err)
		}
		for _, entry := range r.Entries {
			if entry.Path == "link.par2" && (entry.PAR2 != nil || entry.Kind != "link") {
				t.Fatal(entry)
			}
		}
	}
}

func TestPAR2MetadataCountersKeepReadFailures(t *testing.T) {
	run := newPAR2Run()
	var report Report
	r := &fileReader{ctx: context.Background(), f: bytes.NewReader([]byte("PAR2\x00PKT")), limits: par2Limits(), report: &report, par2: run}
	m, _, err := inspectPAR2(context.Background(), r, 100, true)
	if !errors.Is(err, io.EOF) || m.HeadersComplete || report.BytesRead != 8 || run.report.BytesRead != 8 {
		t.Fatal(m, err, report)
	}
	if !reflect.DeepEqual(run.report.Sets, []PAR2Set{}) {
		t.Fatal(run.report.Sets)
	}
}

func largeMainFixture() []byte {
	body := make([]byte, 12+16*par2MainIDs)
	binary.LittleEndian.PutUint64(body, 4096)
	binary.LittleEndian.PutUint32(body[8:], par2MainIDs)
	for i := 0; i < par2MainIDs; i++ {
		binary.LittleEndian.PutUint32(body[12+i*16:], uint32(i))
	}
	return inventorytest.Packet(20, par2MainType, body)
}

func TestPAR2MetadataReadCeilingsSpanFiles(t *testing.T) {
	packet := largeMainFixture()
	t.Run("one file", func(t *testing.T) {
		dir := t.TempDir()
		writePAR2Files(t, dir, map[string][]byte{"large.par2": bytes.Repeat(packet, 64), "later.par2": inventorytest.Creator(20, "later")})
		r, err := Inspect(context.Background(), dir, par2Limits())
		if err == nil || r.Complete || r.BytesRead > par2FileBytes+100 || !entryPAR2(t, r, "later.par2").HeadersComplete {
			t.Fatal(r, err)
		}
		m := entryPAR2(t, r, "large.par2")
		if m.MetadataComplete || m.Headers != m.Matching+m.Mismatched+m.Unchecked || m.Unchecked != 1 || m.HeadersComplete {
			t.Fatal("the refused packet lost its unchecked coverage", m)
		}
	})
	t.Run("whole run", func(t *testing.T) {
		dir := t.TempDir()
		files := map[string][]byte{}
		for i := 0; i < 18; i++ {
			files[fmt.Sprintf("%02d.par2", i)] = bytes.Repeat(packet, 63)
		}
		writePAR2Files(t, dir, files)
		r, err := Inspect(context.Background(), dir, par2Limits())
		if err == nil || r.Complete || r.BytesRead > par2TotalBytes || r.PAR2.BytesRead != r.BytesRead || r.PAR2.Packets >= par2Packets || len(r.Entries) != len(files) {
			t.Fatal(r, err)
		}
		if last := entryPAR2(t, r, "17.par2"); last.State != "not_inspected" || last.Packets != 0 {
			t.Fatal("the next file reset the run's metadata read allowance", last)
		}
	})
}

func TestPAR2InsufficientMetadataBodiesAndOrdinaryShortFiles(t *testing.T) {
	dir := t.TempDir()
	invalid := inventorytest.File(
		inventorytest.Packet(21, par2MainType, []byte("short")),
		inventorytest.Main(21, 4096, 1, 1, 1),
		inventorytest.Packet(21, par2DescType, make([]byte, 56)),
		inventorytest.Description(21, 1, []byte("name\x00inside"), 100),
		inventorytest.Creator(21, string([]byte{0xff, 0xfe})))
	tooLarge := inventorytest.Description(21, 2, bytes.Repeat([]byte("a"), par2NameBytes+20), 100)
	writePAR2Files(t, dir, map[string][]byte{"invalid.par2": invalid, "long.par2": tooLarge, "ordinary.txt": []byte("PAR")})
	r, err := Inspect(context.Background(), dir, par2Limits())
	if err == nil || r.Complete || entryPAR2(t, r, "invalid.par2").Matching != 5 || entryPAR2(t, r, "long.par2").Unchecked != 1 {
		t.Fatal(r, err)
	}
	for _, entry := range r.Entries {
		if entry.Path == "ordinary.txt" && (entry.PAR2 != nil || entry.Finding != "") {
			t.Fatal("short ordinary content became a recovery claim", entry)
		}
	}
	set := r.PAR2.Sets[0]
	if len(set.Mains) != 0 || len(set.Descriptions) != 0 || len(set.Creators) != 1 || set.Creators[0].TextHex != "fffe" {
		t.Fatal("invalid declarations were accepted or opaque bytes were lost", set)
	}
	if findings := entryPAR2(t, r, "invalid.par2").Findings; len(findings) != 4 {
		t.Fatal(findings)
	}
}

func TestPAR2CancellationDuringAReadKeepsMeasuredWork(t *testing.T) {
	dir := t.TempDir()
	writePAR2Files(t, dir, map[string][]byte{"cancel.par2": inventorytest.Creator(22, "cancel")})
	f, err := os.Open(filepath.Join(dir, "cancel.par2"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wrapped := &mutateInventoryFile{File: f, onRead: cancel}
	run := newPAR2Run()
	var report Report
	sum, metadata, observations, err := readOpened(ctx, wrapped, before, "cancel.par2", par2Limits(), &report, run)
	if !errors.Is(err, context.Canceled) || sum != "" || metadata.HeadersComplete || len(observations) != 0 || report.BytesRead != 8 || run.report.Packets != 0 {
		t.Fatal(sum, metadata, observations, report, err)
	}
}

func TestPAR2MetadataCoverageIsIndependentOfARequestedWholeFileHash(t *testing.T) {
	dir := t.TempDir()
	body := inventorytest.Creator(23, "metadata")
	writePAR2Files(t, dir, map[string][]byte{"one.par2": body})
	l := par2Limits()
	l.Hashes = true
	l.TotalBytes = int64(len(body))
	r, err := Inspect(context.Background(), dir, l)
	if err == nil || r.Complete || r.Entries[0].SHA256 != "" || r.BytesRead != int64(len(body)) {
		t.Fatal(r, err)
	}
	if !r.PAR2.Complete || !r.Entries[0].PAR2.HeadersComplete || !r.Entries[0].PAR2.MetadataComplete || r.Entries[0].PAR2.Matching != 1 {
		t.Fatal("a whole-file hash refusal changed completed metadata coverage", r.PAR2, r.Entries[0])
	}
}

func TestPAR2SignatureProbeMakesNoClaimAboutLaterPayload(t *testing.T) {
	dir := t.TempDir()
	body := append([]byte("prefix!!"), inventorytest.Creator(24, "later packet")...)
	writePAR2Files(t, dir, map[string][]byte{"prefixed.par2": body})
	r, err := Inspect(context.Background(), dir, par2Limits())
	if err != nil || !r.Complete || !r.PAR2.Complete || r.BytesRead != 8 || len(r.PAR2.Sets) != 0 {
		t.Fatal(r, err)
	}
	m := entryPAR2(t, r, "prefixed.par2")
	if m.State != "signature_not_matched" || !m.ProbeComplete || m.HeadersComplete || m.Packets != 0 {
		t.Fatal("an offset-zero probe claimed whole-file format coverage", m)
	}
}

func FuzzPAR2MetadataStaysBounded(f *testing.F) {
	for _, seed := range [][]byte{nil, []byte("PAR2\x00PKT"), make([]byte, 64), inventorytest.Main(1, 4096, 1, 1), inventorytest.Description(1, 1, []byte("../untrusted.epub"), ^uint64(0)), inventorytest.Creator(1, "synthetic"), mutatePAR2Length(inventorytest.Creator(1, "length"), ^uint64(0))} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			t.Skip()
		}
		before := append([]byte{}, data...)
		run := newPAR2Run()
		var report Report
		limits := par2Limits()
		limits.FileBytes, limits.TotalBytes = 65536, 65536
		r := &fileReader{ctx: context.Background(), f: bytes.NewReader(data), limits: limits, report: &report, par2: run}
		metadata, observations, _ := inspectPAR2(context.Background(), r, int64(len(data)), true)
		if metadata != nil {
			run.add("synthetic.par2", metadata, observations)
		}
		run.finish()
		if report.BytesRead > limits.TotalBytes || run.report.BytesRead != report.BytesRead || run.report.Packets > par2FilePackets || len(run.report.Sets) > par2Sets || !bytes.Equal(data, before) {
			t.Fatal("metadata escaped its limits or mutated source bytes", report, run.report)
		}
	})
}
