package tui

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/inventory"
)

func mp4Box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return append(append(binary.BigEndian.AppendUint32(nil, uint32(8+len(body))), typ...), body...)
}

func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }

// mp4Book returns an M4B of 60 AAC samples with two Nero chapters.
func mp4Book() []byte {
	const n = 60
	ftyp := mp4Box("ftyp", []byte("M4B "), be32(0))
	entry := mp4Box("mp4a", make([]byte, 6), []byte{0, 1}, make([]byte, 8), []byte{0, 1, 0, 16}, make([]byte, 4), be32(44100<<16),
		mp4Box("esds", make([]byte, 4), []byte{0x03, 22, 0, 1, 0, 0x04, 17, 0x40, 0x15}, make([]byte, 11), []byte{0x05, 2, 0x12, 0x08}))
	chpl := mp4Box("chpl", []byte{1, 0, 0, 0}, make([]byte, 4), []byte{2}, make([]byte, 8), []byte{7}, []byte("Opening"), binary.BigEndian.AppendUint64(nil, 5_000_000), []byte{3}, []byte("End"))
	moov := func(offset uint32) []byte {
		return mp4Box("moov",
			mp4Box("mvhd", make([]byte, 12), be32(1000), be32(n*1024*1000/44100), make([]byte, 80)),
			mp4Box("trak", mp4Box("tkhd", make([]byte, 12), be32(1), make([]byte, 64)), mp4Box("mdia",
				mp4Box("mdhd", make([]byte, 12), be32(44100), be32(n*1024), make([]byte, 4)),
				mp4Box("hdlr", make([]byte, 8), []byte("soun"), make([]byte, 12)),
				mp4Box("minf", mp4Box("stbl",
					mp4Box("stsd", make([]byte, 4), be32(1), entry),
					mp4Box("stts", make([]byte, 4), be32(1), be32(n), be32(1024)),
					mp4Box("stsc", make([]byte, 4), be32(1), be32(1), be32(n), be32(1)),
					mp4Box("stsz", make([]byte, 4), be32(200), be32(n)),
					mp4Box("stco", make([]byte, 4), be32(1), be32(offset)))))),
			mp4Box("udta", chpl))
	}
	offset := uint32(len(ftyp) + len(moov(0)) + 8)
	return slices.Concat(ftyp, moov(offset), mp4Box("mdat", bytes.Repeat([]byte{0x5a}, 200*n)))
}

// An M4B holding lists its sample tables and chapters, and a text read is
// refused with the way to play it instead.
func TestHoldingsStageM4BForChapters(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "book.m4b"), mp4Book(), 0600); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(t.TempDir(), "catalog.json")
	service := app.New()
	if _, err := service.Snapshot(context.Background(), root, catalog, inventory.Defaults(), false); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), service)
	m.switchMode("holdings")
	m.format = "all"
	m.input.SetValue(catalog)
	m.Update(m.start(true)())
	m.Update(key(tea.KeyF6))
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.reader.representation != "mp4-audio/1" {
		t.Fatal("M4B holding staged as text", m.reader)
	}
	m.secondary.SetValue(root)
	m.Update(m.start(true)())
	if len(m.entries) != 3 || !strings.Contains(m.entries[0].title, "MP4 audio: aac") || m.entries[1].title != "Chapter: Opening" || m.entries[2].summary != "0:00:00.500 to 0:00:01.393" {
		t.Fatal("chapters missing", m.status, len(m.entries), m.report)
	}
	if m.startRead(false, 0, "", false) != nil || !strings.Contains(m.status, "opens a verified copy in a player") {
		t.Fatal("an M4B read was attempted", m.status)
	}
}
