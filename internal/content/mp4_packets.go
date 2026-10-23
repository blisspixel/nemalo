package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/blisspixel/nemalo/internal/audio"
	"github.com/blisspixel/nemalo/internal/safeio"
)

const MP4PacketRepresentation = "mp4-audio/2"

// MP4Initialization is one original, bounded, untrusted sample entry, not a
// playable container or a claim that a decoder accepted the configuration.
type MP4Initialization struct {
	Kind        string `json:"kind"`
	STSDVersion byte   `json:"stsd_version"`
	SHA256      string `json:"sha256"`
	Encoding    string `json:"encoding"`
	Data        []byte `json:"data"`
}

type MP4PacketLocation struct {
	Index       int    `json:"index"`
	ByteStart   int64  `json:"byte_start"`
	ByteEnd     int64  `json:"byte_end"`
	DecodeStart string `json:"decode_start"`
	DecodeEnd   string `json:"decode_end"`
}

// MP4PacketAccess keeps original packet ordinals and media-table ticks apart
// from decoder output and player presentation. Decimal strings preserve ticks
// beyond the exact numeric range of common JSON consumers.
type MP4PacketAccess struct {
	Codec            string              `json:"codec"`
	PacketCount      int                 `json:"packet_count"`
	DeclaredRate     int                 `json:"declared_sample_rate"`
	DeclaredChannels int                 `json:"declared_channels"`
	Initialization   MP4Initialization   `json:"initialization"`
	Timescale        uint32              `json:"timescale"`
	Timeline         string              `json:"timeline"`
	Packets          []MP4PacketLocation `json:"packets,omitempty"`
	NextPacket       int                 `json:"next_packet"`
	EndOfTrack       bool                `json:"end_of_track"`
	Stop             string              `json:"stop,omitempty"`
	DecoderPreroll   string              `json:"decoder_preroll"`
	Presentation     string              `json:"presentation_mapping"`
	PCMEquivalence   string              `json:"pcm_equivalence"`
}

func getMP4Packets(ctx context.Context, req Request, result Result) (Result, error) {
	result.Extractor, result.Representation = audio.MP4PacketExtractor, MP4PacketRepresentation
	result.Limitations = []string{
		"Original encoded packets and an untrusted source sample entry, not a playable M4B container or decoded waveform.",
		"Packet ordinals and declared media decode ticks are not PCM samples or edited player times. Decoder initialization, preroll, presentation mapping and PCM equivalence are not verified.",
		"Each call prepares a verified private copy up to 2 GiB and rehashes the original. Temporary disk may approach 2 GiB; original-source reads may approach 4 GiB even for a small result.",
		"Packet data is bounded to 64 KiB and 128 packets; initialization to 16 KiB. Rights, safety, semantic completeness and consumption are not established.",
	}
	if req.Unit != 0 || req.Offset != 0 || req.Part != "" || req.Resource || req.Sample != 0 {
		return result, errors.New("mp4-audio/2 has one audio unit; select an original packet or a returned cursor")
	}
	var c cursor
	var err error
	if req.Cursor != "" {
		c, err = parseCursor(req.Cursor)
		if err != nil {
			return result, err
		}
		if c.AssetID != req.AssetID || c.Extractor != audio.MP4PacketExtractor || c.Representation != MP4PacketRepresentation || c.Unit != 0 || c.Part != "" || c.Member != "" || c.MemberSHA256 != "" {
			result.Status = "stale_reference"
			return result, errors.New("packet continuation belongs to another asset, extractor or representation")
		}
	}
	first := c.Offset
	if req.Packet != nil {
		first = *req.Packet
	}
	var summary audio.MP4
	var access MP4PacketAccess
	var selected audio.MP4PacketRange
	var representationID, failureStatus string
	err = resolveFile(ctx, req, &result, func(source io.ReaderAt, size int64) error {
		return withPacketSnapshot(ctx, source, size, req.AssetID[len("sha256:"):], func(snapshot io.ReaderAt) error {
			index, err := audio.IndexMP4(ctx, snapshot, size)
			if err != nil {
				failureStatus = "malformed_content"
				if errors.Is(err, audio.ErrMP4PacketUnsupported) || errors.Is(err, audio.ErrNotMP4) {
					failureStatus = "unsupported_extraction"
				}
				return err
			}
			entry, version, err := index.SampleDescription()
			if err != nil {
				failureStatus = "unsupported_extraction"
				return err
			}
			entryHash := sha256.Sum256(entry)
			access = MP4PacketAccess{
				Codec: index.Summary.Codec, PacketCount: index.Summary.Samples, DeclaredRate: index.Summary.SampleRate, DeclaredChannels: index.Summary.Channels,
				Initialization: MP4Initialization{Kind: "original_audio_sample_entry", STSDVersion: version, SHA256: hex.EncodeToString(entryHash[:]), Encoding: "base64", Data: entry},
				Timescale:      index.Summary.Timescale, Timeline: "media_decode_ticks", DecoderPreroll: "not_established", Presentation: "not_applied", PCMEquivalence: "not_established",
			}
			encoded, _ := json.Marshal(struct {
				Asset, Extractor, Representation, Initialization string
			}{req.AssetID, audio.MP4PacketExtractor, MP4PacketRepresentation, access.Initialization.SHA256})
			hash := sha256.Sum256(encoded)
			representationID = "sha256:" + hex.EncodeToString(hash[:])
			if req.Cursor != "" && c.RepresentationID != representationID {
				failureStatus = "stale_reference"
				return errors.New("packet continuation representation changed")
			}
			summary = index.Summary
			if req.UnitsOnly {
				return nil
			}
			if first >= summary.Samples {
				failureStatus = "invalid_request"
				if req.Cursor != "" {
					failureStatus = "stale_reference"
				}
				return errors.New("packet ordinal is outside the selected audio track")
			}
			selected, err = index.ReadRange(ctx, first, req.MaxBytes)
			if err != nil {
				failureStatus = "unsupported_extraction"
				if errors.Is(err, audio.ErrMP4PacketRangeTooSmall) {
					failureStatus = "invalid_request"
					return fmt.Errorf("%w; the first packet requires at least %d bytes", err, selected.RequiredBytes)
				}
				return err
			}
			return nil
		})
	})
	if err != nil {
		if result.Status != "changed_source" && result.Status != "missing_asset" {
			switch {
			case ctx.Err() != nil:
				result.Status, err = "cancelled", ctx.Err()
			case errors.Is(err, errPacketSnapshotChanged):
				result.Status = "changed_source"
			case errors.Is(err, errBudget), errors.Is(err, audio.ErrBudget), errors.Is(err, safeio.ErrNoSpace):
				result.Status = "budget_exceeded"
			case failureStatus != "":
				result.Status = failureStatus
			}
		}
		return result, err
	}
	result.RepresentationID = representationID
	result.Units = []Unit{{Index: 0, Kind: "mp4_audio_track", Linear: "yes", Extraction: "packet_table", Language: summary.Language}}
	result.MP4Packets = &access
	if req.UnitsOnly {
		result.Status = "units_available"
		return finishStructured(ctx, result)
	}
	access.NextPacket, access.EndOfTrack, access.Stop = selected.NextPacket, selected.EndOfTrack, selected.Stop
	for _, p := range selected.Packets {
		access.Packets = append(access.Packets, MP4PacketLocation{Index: p.Index, ByteStart: p.ByteStart, ByteEnd: p.ByteEnd, DecodeStart: strconv.FormatInt(p.DecodeStart, 10), DecodeEnd: strconv.FormatInt(p.DecodeEnd, 10)})
	}
	payloadHash := sha256.Sum256(selected.Data)
	result.Resource = &ResourceData{Member: "encoded_packets", SHA256: hex.EncodeToString(payloadHash[:]), TotalBytes: len(selected.Data), Encoding: "base64", Data: selected.Data}
	result.Locator = &Locator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: audio.MP4PacketExtractor, RepresentationID: representationID, Unit: 0, Start: first, End: selected.NextPacket, OffsetUnit: "mp4_packets"}
	result.EndOfUnit, result.EndOfSource = selected.EndOfTrack, selected.EndOfTrack
	result.Status = "complete_range"
	if !selected.EndOfTrack {
		result.Status = "partial_range"
		result.Continuation = encodeCursor(cursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: audio.MP4PacketExtractor, RepresentationID: representationID, Offset: selected.NextPacket, Representation: MP4PacketRepresentation})
	}
	result.Coverage = &Coverage{Text: "not_applicable", KnownGaps: []string{}, ReferencedOutsideRange: []string{}, Unknown: []string{"Only selected original packets were returned. Decoder output, edited presentation time, audible content and unselected packet coverage are not established."}}
	if selected.Stop == "unsupported_empty_packet" {
		result.Coverage.KnownGaps = append(result.Coverage.KnownGaps, "empty_packet_at_"+strconv.Itoa(selected.NextPacket))
	}
	return finishStructured(ctx, result)
}
