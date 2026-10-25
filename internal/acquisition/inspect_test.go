package acquisition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/discovery"
)

func writeJSON(t *testing.T, dir, name string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// An existing packet is importable only with a completed intake receipt,
// resumable only as acquire --resume could continue it, and otherwise
// retained with its reason.
func TestInspectPacketReadsWhatAPacketHolds(t *testing.T) {
	packet := func() string {
		d := t.TempDir()
		return d
	}
	done := packet()
	writeJSON(t, done, "receipt.json", Result{SchemaVersion: 1, Complete: true, Status: "untrusted_intake", Intent: &Intent{Request: Request{SourceID: "archive:a", File: "a.epub"}}})
	if s := InspectPacket(done); s.State != PacketImportable || s.SourceID != "archive:a" || s.File != "a.epub" {
		t.Fatal(s)
	}
	detected := packet()
	writeJSON(t, detected, "receipt.json", Result{Status: "detected_quarantined"})
	if s := InspectPacket(detected); s.State != PacketRetained || !strings.Contains(s.Detail, "detected_quarantined") {
		t.Fatal(s)
	}
	n := int64(100)
	file := discovery.OfferedFile{Name: "a.epub", Format: "EPUB", Bytes: &n}
	partial := packet()
	writeJSON(t, partial, "intent.json", Intent{SchemaVersion: 1, Request: Request{SourceID: "archive:a", File: "a.epub"}, Selection: discovery.Evaluation{Files: []discovery.OfferedFile{file}}})
	writeJSON(t, partial, "transfer.json", transferRecord{SchemaVersion: 1, Status: 200, ETag: `"abc"`, StrongETag: true, File: "a.epub"})
	if err := os.WriteFile(filepath.Join(partial, "content.epub"), make([]byte, 40), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := InspectPacket(partial); s.State != PacketResumable || s.Remaining != 60 || s.SourceID != "archive:a" || s.File != "a.epub" {
		t.Fatal(s)
	}
	weak := packet()
	writeJSON(t, weak, "intent.json", Intent{SchemaVersion: 1, Request: Request{SourceID: "archive:a", File: "a.epub"}, Selection: discovery.Evaluation{Files: []discovery.OfferedFile{file}}})
	writeJSON(t, weak, "transfer.json", transferRecord{SchemaVersion: 1, Status: 200, ETag: `W/"abc"`, File: "a.epub"})
	if s := InspectPacket(weak); s.State != PacketRetained {
		t.Fatal(s)
	}
	paper := packet()
	writeJSON(t, paper, "intent.json", Intent{SchemaVersion: 1, Request: Request{SourceID: "arxiv:1706.03762v7", File: "1706.03762v7.pdf"}, Selection: discovery.Evaluation{Files: []discovery.OfferedFile{file}}})
	if s := InspectPacket(paper); s.State != PacketRetained || !strings.Contains(s.Detail, "arXiv") {
		t.Fatal(s)
	}
	if s := InspectPacket(packet()); s.State != PacketRetained {
		t.Fatal(s)
	}
	recording := packet()
	writeJSON(t, recording, recordingIntentName, recordingIntent{Request: RecordingRequest{SourceID: "archive:a", Format: "64Kbps MP3"}, Selection: discovery.Evaluation{Recordings: []discovery.Recording{{Format: "64Kbps MP3", Bytes: 900}}}})
	if s := InspectRecording(recording); s.State != PacketResumable || s.Remaining != 900 || s.SourceID != "archive:a" || s.Recording != "64Kbps MP3" {
		t.Fatal(s)
	}
	writeJSON(t, recording, recordingReceiptName, RecordingResult{SchemaVersion: 1, Complete: true, Status: "untrusted_intake", SourceID: "archive:a", Format: "64Kbps MP3"})
	if s := InspectRecording(recording); s.State != PacketImportable || s.SourceID != "archive:a" || s.Recording != "64Kbps MP3" {
		t.Fatal(s)
	}
	// A receipt of an unknown schema version is never treated as importable.
	writeJSON(t, recording, recordingReceiptName, RecordingResult{SchemaVersion: 2, Complete: true, Status: "untrusted_intake", SourceID: "archive:a", Format: "64Kbps MP3"})
	if s := InspectRecording(recording); s.State != PacketRetained {
		t.Fatal("a newer recording receipt was importable:", s)
	}
	if s := InspectRecording(packet()); s.State != PacketRetained {
		t.Fatal(s)
	}
}
