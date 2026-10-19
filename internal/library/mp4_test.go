package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func mp4Box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), typ...), body...)
}

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// mp4Audiobook returns an MP4 audio file of n AAC samples of 200 bytes, with
// the movie box before the media data.
func mp4Audiobook(n int) []byte {
	ftyp := mp4Box("ftyp", []byte("M4B "), be32(0), []byte("M4B isom"))
	entry := mp4Box("mp4a", make([]byte, 6), []byte{0, 1}, make([]byte, 8), []byte{0, 1, 0, 16}, make([]byte, 4), be32(44100<<16),
		mp4Box("esds", make([]byte, 4), []byte{0x03, 22, 0, 1, 0, 0x04, 17, 0x40, 0x15}, make([]byte, 11), []byte{0x05, 2, 0x12, 0x08}))
	moov := func(offset uint32) []byte {
		return mp4Box("moov",
			mp4Box("mvhd", make([]byte, 12), be32(1000), be32(uint32(n*1024*1000/44100)), make([]byte, 80)),
			mp4Box("trak", mp4Box("tkhd", make([]byte, 12), be32(1), make([]byte, 64)), mp4Box("mdia",
				mp4Box("mdhd", make([]byte, 12), be32(44100), be32(uint32(n*1024)), make([]byte, 4)),
				mp4Box("hdlr", make([]byte, 8), []byte("soun"), make([]byte, 12)),
				mp4Box("minf", mp4Box("stbl",
					mp4Box("stsd", make([]byte, 4), be32(1), entry),
					mp4Box("stts", make([]byte, 4), be32(1), be32(uint32(n)), be32(1024)),
					mp4Box("stsc", make([]byte, 4), be32(1), be32(1), be32(uint32(n)), be32(1)),
					mp4Box("stsz", make([]byte, 4), be32(200), be32(uint32(n))),
					mp4Box("stco", make([]byte, 4), be32(1), be32(offset)))))))
	}
	offset := uint32(len(ftyp) + len(moov(0)) + 8)
	return slices.Concat(ftyp, moov(offset), mp4Box("mdat", bytes.Repeat([]byte{0x5a}, 200*n)))
}

// An M4B or M4A whose every sample lies in its media data is checked after a
// clean scan; one cut short stays in review, and a packet imports its file.
func TestMP4CheckedPolicy(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) string {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	book, err := Import(ctx, ImportRequest{Library: root, Source: write("book.m4b", mp4Audiobook(60)), Apply: true, Scan: true, Scanner: cleanScan{}})
	if err != nil || book.Status != "checked" || book.Reason != "checks_and_scan_recorded" || book.Format != "m4b" || filepath.Ext(book.Path) != ".m4b" {
		t.Fatal(book.Status, book.Reason, book.Format, book.Findings, err)
	}
	data := mp4Audiobook(40)
	cut, err := Import(ctx, ImportRequest{Library: root, Source: write("cut.m4a", data[:len(data)-300]), Apply: true, Scan: true, Scanner: cleanScan{}})
	if err != nil || cut.Status != "review" || !slices.Contains(cut.Findings, "mp4_samples_outside_file") {
		t.Fatal(cut.Status, cut.Reason, cut.Findings, err)
	}
	packet := t.TempDir()
	body := mp4Audiobook(30)
	if err := os.WriteFile(filepath.Join(packet, "content.m4b"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	receipt, err := json.Marshal(map[string]any{"schema_version": 1, "complete": true, "status": "untrusted_intake", "final_url": "https://example.test/book.m4b", "transfer": map[string]any{"bytes": len(body), "sha256": hex.EncodeToString(sum[:])}, "intent": map[string]any{"schema_version": 1, "request": map[string]any{"source_id": "archive:demo", "source_file": "book.m4b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packet, "receipt.json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	held, err := Import(ctx, ImportRequest{Library: root, Source: packet, Apply: true})
	if err != nil || held.SourceKind != "packet" || held.Format != "m4b" || held.Status != "review" || held.Reason != "unscanned" {
		t.Fatal(held, err)
	}
	page, err := FindManagedFiltered(ctx, root, ManagedFilter{Format: "m4b", Status: "all", Limit: 10})
	if err != nil || page.Total != 2 {
		t.Fatal(page.Total, err)
	}
	// The audit rehashes all three and lists only the two in review.
	audit, _ := AuditManaged(ctx, root)
	if len(audit.Findings) != 2 || audit.Findings[0].Status != "review" || audit.Findings[1].Status != "review" {
		t.Fatal(audit.Findings)
	}
}
