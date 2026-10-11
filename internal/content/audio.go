package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/blisspixel/nemalo/internal/audio"
)

// getAudio serves mpeg-audio/1: a frame index of one MP3 file and bounded,
// frame-aligned byte ranges with decoder preroll. It never decodes audio. The
// same request, locator, continuation, and stale-reference rules apply as for
// text; offsets are encoded samples and a cursor names the next frame.
func getAudio(ctx context.Context, req Request, result Result) (Result, error) {
	result.Extractor, result.Representation = audio.Extractor, "mpeg-audio/1"
	result.Limitations = []string{
		"MPEG audio frames are indexed from their headers, not decoded. Encoded sample positions are not verified playback times.",
		"Tags, info frames, chapters, encoder delay, and padding are untrusted declarations.",
		"One file is one track. Recording identity, track order, and completeness across files are not established.",
		"Rights and edition metadata are not established by a byte-identity catalog; unknown.",
		"No antivirus scan, safety guarantee, listening history, or consumption acknowledgement.",
	}
	if req.Part != "" || req.Resource || req.Unit != 0 || req.Offset != 0 {
		return result, errors.New("mpeg-audio/1 has one unit; select a range with sample or a cursor")
	}
	var c cursor
	var err error
	if req.Cursor != "" {
		c, err = parseCursor(req.Cursor)
		if err != nil {
			return result, err
		}
		if c.AssetID != req.AssetID || c.Extractor != audio.Extractor || c.Representation != "mpeg-audio/1" || c.Unit != 0 || c.Part != "" || c.Member != "" || c.MemberSHA256 != "" {
			result.Status = "stale_reference"
			return result, errors.New("continuation belongs to another asset, extractor, or representation")
		}
	}
	data, err := resolveSource(ctx, req, &result)
	if err != nil {
		return result, err
	}
	track, err := audio.Probe(ctx, data)
	if err != nil {
		switch {
		case ctx.Err() != nil:
			result.Status, err = "cancelled", ctx.Err()
		case errors.Is(err, audio.ErrBudget):
			result.Status = "budget_exceeded"
		case errors.Is(err, audio.ErrNotMPEG):
			result.Status = "unsupported_extraction"
		default:
			result.Status = "malformed_content"
		}
		return result, err
	}
	encoded, err := json.Marshal(struct {
		Extractor string
		Track     audio.Track
		Offsets   []int64
	}{audio.Extractor, track, track.Offsets()})
	if err != nil {
		return result, err
	}
	hash := sha256.Sum256(encoded)
	result.RepresentationID = "sha256:" + hex.EncodeToString(hash[:])
	result.Units = []Unit{{Index: 0, Kind: "mpeg_audio_track", Linear: "yes", Extraction: "frame_index", Language: track.Metadata.Language}}
	if req.Cursor != "" && c.RepresentationID != result.RepresentationID {
		result.Status = "stale_reference"
		return result, errors.New("continuation representation changed")
	}
	if req.UnitsOnly {
		result.Status, result.Audio = "units_available", &track
		return finishStructured(ctx, result)
	}
	first := c.Offset
	if req.Cursor == "" {
		if first, err = track.FrameForSample(req.Sample); err != nil {
			result.Status = "invalid_request"
			return result, err
		}
	}
	r, err := track.Range(data, first, req.MaxBytes)
	if err != nil {
		result.Status = "invalid_request"
		if errors.Is(err, audio.ErrRangeTooSmall) {
			err = fmt.Errorf("%w; use max-bytes of at least %d", err, r.MinimumMaxBytes)
		}
		return result, err
	}
	result.AudioRange = &r
	result.Resource = &ResourceData{SHA256: req.AssetID[len("sha256:"):], TotalBytes: len(data), Encoding: "base64", Data: data[r.ByteStart:r.ByteEnd]}
	result.Locator = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: audio.Extractor, RepresentationID: result.RepresentationID, Unit: 0, Start: int(r.SampleStart), End: int(r.SampleEnd), OffsetUnit: "encoded_samples"}
	result.EndOfUnit, result.EndOfSource = r.EndOfTrack, r.EndOfTrack
	result.Status = "complete_range"
	if !r.EndOfTrack {
		result.Status = "partial_range"
		result.Continuation = encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: audio.Extractor, RepresentationID: result.RepresentationID, Unit: 0, Offset: r.NextFrame, Representation: "mpeg-audio/1"})
	}
	result.Coverage = &Coverage{Text: "not_applicable", KnownGaps: []string{}, ReferencedOutsideRange: []string{}, Unknown: []string{"No audio was decoded; decoder delay, gapless trimming, and audible content are not established."}}
	if r.StoppedAtGap {
		result.Coverage.KnownGaps = append(result.Coverage.KnownGaps, "unindexed_bytes_follow_this_range")
	}
	if !r.PrerollComplete {
		result.Coverage.KnownGaps = append(result.Coverage.KnownGaps, "decoder_preroll_incomplete")
	}
	return finishStructured(ctx, result)
}
