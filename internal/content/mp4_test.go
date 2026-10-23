package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/audio/audiotest"
	"github.com/blisspixel/nemalo/internal/library"
)

// mp4Audio returns an M4B of n AAC samples of 200 bytes, each 1024 units at
// 44,100 Hz, with two Nero chapters.
func mp4Audio(n int) []byte {
	return audiotest.MP4(n)
}

func TestMP4UnitsListChaptersAndRefuseRanges(t *testing.T) {
	req := audioFixture(t, mp4Audio(60))
	req.Representation, req.UnitsOnly = "mp4-audio/1", true
	r, err := Get(context.Background(), req)
	if err != nil || r.Status != "units_available" || r.MP4 == nil || r.MP4.Samples != 60 || len(r.MP4.Chapters) != 2 || r.MP4.Chapters[1].StartMs != 500 || r.MP4.Chapters[1].EndMs != 1393 || len(r.Units) != 1 || r.Units[0].Kind != "mp4_audio_track" || !validRepresentationID(r.RepresentationID) {
		t.Fatal(r, err)
	}
	again, _ := Get(context.Background(), req)
	if again.RepresentationID != r.RepresentationID {
		t.Fatal("the same bytes gave another representation")
	}
	req.UnitsOnly = false
	if r, err := Get(context.Background(), req); err == nil || r.MP4 != nil {
		t.Fatal("mp4-audio/1 served a range", r.Status)
	}
	// An MP3 has no MP4 sample tables.
	req = audioFixture(t, mp3Frames(10))
	req.Representation, req.UnitsOnly = "mp4-audio/1", true
	if r, err := Get(context.Background(), req); err == nil || r.Status != "unsupported_extraction" || r.MP4 != nil {
		t.Fatal(r.Status, err)
	}
}

func validRepresentationID(id string) bool {
	return len(id) == 71 && id[:7] == "sha256:"
}

// A file rewritten while it is read at offsets, even with its size and time
// restored, is refused by the hash that follows the read.
func TestOffsetReadsRefuseAFileChangedWhileRead(t *testing.T) {
	data := mp4Audio(20)
	req := audioFixture(t, data)
	path := filepath.Join(req.Root, "track.mp3")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	asset := library.Asset{ID: req.AssetID, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
	status, err := useAsset(context.Background(), req.Root, "track.mp3", asset, func(r io.ReaderAt, size int64) error {
		changed := bytes.Clone(data)
		changed[len(changed)-1] ^= 0xff
		if err := os.WriteFile(path, changed, 0o600); err != nil {
			return err
		}
		return os.Chtimes(path, info.ModTime(), info.ModTime())
	})
	if err == nil || status != "changed_source" {
		t.Fatal(status, err)
	}
	if status, err := useAsset(context.Background(), req.Root, "track.mp3", library.Asset{ID: req.AssetID, SHA256: asset.SHA256, Bytes: assessment.MaxMP4Bytes + 1}, func(io.ReaderAt, int64) error { return nil }); status != "budget_exceeded" || err == nil {
		t.Fatal(status, err)
	}
}

// A read that fails because the file changed under it reports the change.
func TestOffsetReadFailureOnAChangedFileIsAChange(t *testing.T) {
	data := mp4Audio(20)
	req := audioFixture(t, data)
	path := filepath.Join(req.Root, "track.mp3")
	sum := sha256.Sum256(data)
	asset := library.Asset{ID: req.AssetID, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}
	status, err := useAsset(context.Background(), req.Root, "track.mp3", asset, func(io.ReaderAt, int64) error {
		if err := os.WriteFile(path, append(bytes.Clone(data), 1), 0o600); err != nil {
			return err
		}
		return errors.New("probe failed")
	})
	if status != "changed_source" || err == nil {
		t.Fatal(status, err)
	}
}
