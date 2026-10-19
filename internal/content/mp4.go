package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/blisspixel/nemalo/internal/audio"
)

// getMP4 serves mp4-audio/1: the sample table summary of one M4B or M4A file,
// with its declared chapters and timing. It lists units only. MP4 samples are
// not playable apart from their container, so a player gets a verified copy
// through library open or export rather than byte ranges from here.
func getMP4(ctx context.Context, req Request, result Result) (Result, error) {
	result.Extractor, result.Representation = audio.MP4Extractor, "mp4-audio/1"
	result.Limitations = []string{
		"MP4 samples are located from the sample tables, not decoded. Encoded timing is the sum of declared sample durations; edit lists and encoder priming are not applied.",
		"Chapters, titles, and other item text are untrusted declarations.",
		"Units only: hand the file to a player with library open or export. No byte ranges are served.",
		"Rights and edition metadata are not established by a byte-identity catalog; unknown.",
		"No antivirus scan, safety guarantee, listening history, or consumption acknowledgement.",
	}
	if req.Part != "" || req.Resource || req.Unit != 0 || req.Offset != 0 || req.Cursor != "" || !req.UnitsOnly {
		return result, errors.New("mp4-audio/1 lists units only; use units, and library open or export to play the file")
	}
	data, err := resolveSource(ctx, req, &result)
	if err != nil {
		return result, err
	}
	m, err := audio.ProbeMP4(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		switch {
		case ctx.Err() != nil:
			result.Status, err = "cancelled", ctx.Err()
		case errors.Is(err, audio.ErrBudget):
			result.Status = "budget_exceeded"
		case errors.Is(err, audio.ErrNotMP4):
			result.Status = "unsupported_extraction"
		default:
			result.Status = "malformed_content"
		}
		return result, err
	}
	encoded, err := json.Marshal(struct {
		Extractor string
		MP4       audio.MP4
	}{audio.MP4Extractor, m})
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(encoded)
	result.RepresentationID = "sha256:" + hex.EncodeToString(hash[:])
	result.Units = []Unit{{Index: 0, Kind: "mp4_audio_track", Linear: "yes", Extraction: "sample_table", Language: m.Language}}
	result.Status, result.MP4 = "units_available", &m
	return finishStructured(ctx, result)
}
