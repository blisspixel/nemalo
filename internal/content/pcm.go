package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/blisspixel/nemalo/internal/audio"
	"github.com/blisspixel/nemalo/internal/decode"
	"github.com/blisspixel/nemalo/internal/safeio"
)

const PCMRepresentation = "mp4-pcm/1"
const PCMExtractor = "nemalo.ffmpeg.pcm/1"
const pcmCursorPrefix = "mp4pcm1:"
const maxPCMSamples int64 = 4 << 30

// PCMDecoder is an internal trusted adapter dependency, never a permission or
// caller-provided report. Every invocation still requires Request.Decode.
type PCMDecoder func(context.Context, string, string, int64, decode.PCMRequest) (decode.PCMRange, error)

type PCMSourceClock struct {
	MediaTimescale     uint32 `json:"media_timescale"`
	DeclaredMediaTicks string `json:"declared_media_ticks"`
	EncodedMediaTicks  string `json:"encoded_media_ticks"`
	MovieTimescale     uint32 `json:"movie_timescale"`
	DeclaredMovieTicks string `json:"declared_movie_ticks"`
	EditList           bool   `json:"edit_list_declared"`
}

type PCMLocator struct {
	SchemaVersion    int    `json:"schema_version"`
	AssetID          string `json:"asset_id"`
	Extractor        string `json:"extractor"`
	RepresentationID string `json:"representation_id"`
	Unit             int    `json:"unit"`
	StartSample      string `json:"start_sample"`
	EndSample        string `json:"end_sample"`
	OffsetUnit       string `json:"offset_unit"`
}

type DecodedMP4 struct {
	Tool                      string         `json:"tool"`
	ToolVersion               string         `json:"tool_version"`
	OutputFormat              string         `json:"output_format"`
	OutputRate                int            `json:"output_sample_rate"`
	OutputChannels            int            `json:"output_channels"`
	ProbedChannels            int            `json:"probed_channels"`
	ContainerReportedRate     int            `json:"container_reported_rate"`
	ContainerReportedChannels int            `json:"container_reported_channels"`
	SourceClock               PCMSourceClock `json:"source_clock"`
	TotalSamples              string         `json:"total_samples"`
	FullPCMSHA256             string         `json:"full_pcm_sha256"`
	InitializationSHA256      string         `json:"source_initialization_sha256"`
	Timeline                  string         `json:"timeline"`
	PresentationMapping       string         `json:"presentation_mapping"`
	Transform                 string         `json:"transform"`
	Range                     *PCMLocator    `json:"range,omitempty"`
	RangePCMSHA256            string         `json:"range_pcm_sha256,omitempty"`
}

type pcmCursor struct {
	SchemaVersion    int    `json:"schema_version"`
	AssetID          string `json:"asset_id"`
	Extractor        string `json:"extractor"`
	RepresentationID string `json:"representation_id"`
	NextSample       string `json:"next_sample"`
}

func validatePCMRequest(r Request) error {
	cursor := strings.HasPrefix(r.Cursor, pcmCursorPrefix)
	selected := r.Representation == PCMRepresentation || r.Representation == "" && cursor
	if r.Decode && !selected || selected && !r.Decode {
		return errors.New("mp4-pcm/1 requires explicit decode permission on every invocation; decode applies only to that representation")
	}
	if r.PCMSample != nil && (!selected || !r.Decode || *r.PCMSample < 0 || *r.PCMSample >= maxPCMSamples || r.UnitsOnly || r.Cursor != "") {
		return errors.New("pcm-sample selects an mp4-pcm/1 read, must be 0-4294967295, and cannot accompany units or a cursor")
	}
	return nil
}

func encodePCMCursor(c pcmCursor) string {
	b, _ := json.Marshal(c)
	return pcmCursorPrefix + base64.RawURLEncoding.EncodeToString(b)
}
func parsePCMCursor(token string) (pcmCursor, int64, error) {
	var c pcmCursor
	if !strings.HasPrefix(token, pcmCursorPrefix) {
		return c, 0, errors.New("invalid decoded-audio continuation prefix")
	}
	raw := strings.TrimPrefix(token, pcmCursorPrefix)
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, 0, errors.New("invalid decoded-audio continuation encoding")
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, 0, err
	}
	canonical, _ := json.Marshal(c)
	n, err := strconv.ParseInt(c.NextSample, 10, 64)
	if err != nil || n < 0 || n >= maxPCMSamples || strconv.FormatInt(n, 10) != c.NextSample || c.SchemaVersion != 1 || !bytes.Equal(b, canonical) || base64.RawURLEncoding.EncodeToString(b) != raw {
		return c, 0, errors.New("invalid decoded-audio continuation schema or fields")
	}
	return c, n, nil
}

func getPCM(ctx context.Context, req Request, result Result, decoder PCMDecoder) (Result, error) {
	result.Extractor, result.Representation = PCMExtractor, PCMRepresentation
	result.Limitations = []string{
		"Explicit full sequential FFmpeg decode to mono signed 16-bit little-endian output. Downmix, quantization, selected output rate and decoder trimming are part of this derived representation.",
		"Samples identify actual decoder output, not original packets, chapters or independently established edited presentation times. Source header values remain declarations, including possible unknown-duration sentinels.",
		"Every call can prepare 2 GiB private disk, read 4 GiB of original bytes and 2 GiB of private revalidation, and stream up to 8 GiB PCM before returning at most 64 KiB WAV. No full waveform is stored.",
		"A five-minute ceiling and output/thread/input limits do not establish an OS sandbox or measure aggregate child memory and cumulative internal file reads. Rights, safety, semantic completeness and consumption remain unknown.",
	}
	if !req.Decode || req.Unit != 0 || req.Offset != 0 || req.Sample != 0 || req.Packet != nil || req.Part != "" || req.Resource {
		return result, errors.New("decoded access requires explicit permission, unit 0 and a decoded sample selector or its cursor")
	}
	if !req.UnitsOnly && req.MaxBytes < 46 {
		return result, errors.New("decoded WAV needs at least 46 bytes for its header and one whole sample")
	}
	if decoder == nil {
		decoder = decode.ReadPCM
	}
	first := int64(0)
	if req.PCMSample != nil {
		first = *req.PCMSample
	}
	var reference pcmCursor
	if req.Cursor != "" {
		c, n, err := parsePCMCursor(req.Cursor)
		if err != nil {
			return result, err
		}
		if c.AssetID != req.AssetID || c.Extractor != PCMExtractor {
			result.Status = "stale_reference"
			return result, errors.New("decoded continuation belongs to another asset or extractor")
		}
		reference, first = c, n
	}
	var meta DecodedMP4
	var selected decode.PCMRange
	var representationID, failure string
	err := resolveFile(ctx, req, &result, func(source io.ReaderAt, size int64) error {
		return withMP4Snapshot(ctx, source, size, req.AssetID[7:], func(snapshot *mp4Snapshot) error {
			index, err := audio.IndexMP4(ctx, snapshot.reader, size)
			if err != nil {
				failure = "malformed_content"
				if errors.Is(err, audio.ErrMP4PacketUnsupported) || errors.Is(err, audio.ErrNotMP4) {
					failure = "unsupported_extraction"
				}
				return err
			}
			entry, _, err := index.SampleDescription()
			if err != nil {
				failure = "unsupported_extraction"
				return err
			}
			clock, err := index.Clock()
			if err != nil {
				return err
			}
			path, err := filepath.Abs(snapshot.path)
			if err != nil {
				return err
			}
			selection := decode.PCMRequest{FirstSample: first, UnitsOnly: req.UnitsOnly}
			if !req.UnitsOnly {
				selection.MaxBytes = req.MaxBytes - 44
			}
			selected, err = decoder(ctx, path, "m4a", size, selection)
			if checked := snapshot.revalidate(ctx); checked != nil {
				return checked
			}
			if err != nil {
				if selected.Measurement.Detail != "" {
					err = fmt.Errorf("%w: %s", err, selected.Measurement.Detail)
				}
				switch {
				case errors.Is(err, decode.ErrPCMBudget):
					failure = "budget_exceeded"
				case errors.Is(err, decode.ErrPCMUnavailable):
					failure = "unavailable"
				case errors.Is(err, decode.ErrPCMRange):
					failure = "invalid_request"
					if req.Cursor != "" {
						failure = "stale_reference"
					}
				default:
					failure = "unsupported_extraction"
				}
				return err
			}
			measurement := selected.Measurement
			hash, err := hex.DecodeString(selected.FullSHA256)
			if err != nil || len(hash) != 32 || hex.EncodeToString(hash) != selected.FullSHA256 || measurement.Status != "passed" || measurement.DecodedSamples < 1 || measurement.DecodedSamples > maxPCMSamples || measurement.SampleRate < 1 || measurement.SampleRate > 1_000_000 || measurement.Channels < 1 || measurement.Channels > 64 || measurement.Version == "" {
				failure = "malformed_content"
				return errors.New("decoder result has incomplete measurement or identity")
			}
			if !req.UnitsOnly && (selected.FirstSample != first || selected.NextSample <= first || selected.NextSample > measurement.DecodedSamples || selected.NextSample-first != int64(len(selected.Data))/2 || len(selected.Data)%2 != 0 || len(selected.Data) > selection.MaxBytes || selected.EndOfStream != (selected.NextSample == measurement.DecodedSamples)) {
				failure = "malformed_content"
				return errors.New("decoder result has inconsistent sample selection")
			}
			entryHash := sha256.Sum256(entry)
			meta = DecodedMP4{Tool: "ffmpeg", ToolVersion: measurement.Version, OutputFormat: "wav_pcm_s16le", OutputRate: measurement.SampleRate, OutputChannels: 1, ProbedChannels: measurement.Channels, ContainerReportedRate: index.Summary.SampleRate, ContainerReportedChannels: index.Summary.Channels, TotalSamples: strconv.FormatInt(measurement.DecodedSamples, 10), FullPCMSHA256: selected.FullSHA256, InitializationSHA256: hex.EncodeToString(entryHash[:]), Timeline: "decoder_output_samples", PresentationMapping: "not_established", Transform: "full sequential decode; mono s16le at selected probed rate; FFmpeg trimming and resampling"}
			meta.SourceClock = PCMSourceClock{MediaTimescale: clock.MediaTimescale, DeclaredMediaTicks: strconv.FormatUint(clock.DeclaredMediaTicks, 10), EncodedMediaTicks: strconv.FormatUint(clock.EncodedMediaTicks, 10), MovieTimescale: clock.MovieTimescale, DeclaredMovieTicks: strconv.FormatUint(clock.DeclaredMovieTicks, 10), EditList: clock.EditList}
			encoded, _ := json.Marshal(struct {
				Asset, Extractor, Representation, Version, Transform, Hash string
				Rate                                                       int
				Samples                                                    string
			}{req.AssetID, PCMExtractor, PCMRepresentation, meta.ToolVersion, meta.Transform, meta.FullPCMSHA256, meta.OutputRate, meta.TotalSamples})
			id := sha256.Sum256(encoded)
			representationID = "sha256:" + hex.EncodeToString(id[:])
			if req.Cursor != "" && reference.RepresentationID != representationID {
				failure = "stale_reference"
				return errors.New("complete decoded output or decoder representation changed")
			}
			return nil
		})
	})
	if err != nil {
		if result.Status != "changed_source" && result.Status != "missing_asset" {
			switch {
			case errors.Is(err, decode.ErrPCMBudget) || errors.Is(err, errBudget) || errors.Is(err, audio.ErrBudget) || errors.Is(err, safeio.ErrNoSpace):
				result.Status = "budget_exceeded"
			case ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
				result.Status = "cancelled"
			case errors.Is(err, errPacketSnapshotChanged):
				result.Status = "changed_source"
			case failure != "":
				result.Status = failure
			}
		}
		return result, err
	}
	result.RepresentationID = representationID
	result.MP4PCM = &meta
	result.Units = []Unit{{Index: 0, Kind: "decoded_audio_track", Linear: "yes", Extraction: "full_sequential_decoder_output"}}
	if req.UnitsOnly {
		result.Status = "units_available"
		return finishStructured(ctx, result)
	}
	pcmHash := sha256.Sum256(selected.Data)
	meta.RangePCMSHA256 = hex.EncodeToString(pcmHash[:])
	meta.Range = &PCMLocator{SchemaVersion: 1, AssetID: req.AssetID, Extractor: PCMExtractor, RepresentationID: representationID, StartSample: strconv.FormatInt(first, 10), EndSample: strconv.FormatInt(selected.NextSample, 10), OffsetUnit: "decoder_output_samples"}
	wave := pcmWAV(meta.OutputRate, selected.Data)
	waveHash := sha256.Sum256(wave)
	result.Resource = &ResourceData{Member: "decoded-range.wav", SHA256: hex.EncodeToString(waveHash[:]), TotalBytes: len(wave), Encoding: "base64", Data: wave}
	result.EndOfUnit, result.EndOfSource = selected.EndOfStream, selected.EndOfStream
	result.Status = "complete_range"
	if !selected.EndOfStream {
		result.Status = "partial_range"
		result.Continuation = encodePCMCursor(pcmCursor{SchemaVersion: 1, AssetID: req.AssetID, Extractor: PCMExtractor, RepresentationID: representationID, NextSample: strconv.FormatInt(selected.NextSample, 10)})
	}
	result.Coverage = &Coverage{Text: "not_applicable", KnownGaps: []string{}, ReferencedOutsideRange: []string{}, Unknown: []string{"Only the selected decoder-output samples were delivered. Mapping to original packets/chapters and semantic or audible completeness are not established."}}
	return finishStructured(ctx, result)
}

func pcmWAV(rate int, data []byte) []byte {
	b := []byte("RIFF")
	b = binary.LittleEndian.AppendUint32(b, uint32(36+len(data)))
	b = append(b, "WAVEfmt "...)
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint32(b, uint32(rate))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate*2))
	b = binary.LittleEndian.AppendUint16(b, 2)
	b = binary.LittleEndian.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	return append(b, data...)
}
