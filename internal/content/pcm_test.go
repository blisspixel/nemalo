package content

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/blisspixel/nemalo/internal/decode"
)

func pcmBytes() []byte {
	b := make([]byte, 3000)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}
func pcmRequest(t *testing.T) Request {
	t.Helper()
	r, _ := packetRequest(t, 5)
	r.Representation = PCMRepresentation
	r.Decode = true
	return r
}
func pcmAdapter(t *testing.T, source, pcm []byte, calls *int, version *string) PCMDecoder {
	t.Helper()
	return func(ctx context.Context, path, format string, size int64, req decode.PCMRequest) (decode.PCMRange, error) {
		*calls++
		if err := ctx.Err(); err != nil {
			return decode.PCMRange{}, err
		}
		copied, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if format != "m4a" || size != int64(len(source)) || !bytes.Equal(copied, source) || filepath.Base(path) != "asset" {
			t.Fatal("decoder did not receive the checked private source", format, path, size)
		}
		m := decode.Result{Status: "passed", Tool: "ffmpeg", Version: *version, SampleRate: 1000, Channels: 2, DecodedSamples: int64(len(pcm) / 2)}
		out := decode.PCMRange{Measurement: m, FullSHA256: digest(pcm)}
		if req.UnitsOnly {
			return out, nil
		}
		if req.FirstSample >= m.DecodedSamples {
			return decode.PCMRange{Measurement: m}, decode.ErrPCMRange
		}
		out.FirstSample = req.FirstSample
		out.NextSample = min(req.FirstSample+int64(req.MaxBytes/2), m.DecodedSamples)
		out.EndOfStream = out.NextSample == m.DecodedSamples
		out.Data = bytes.Clone(pcm[out.FirstSample*2 : out.NextSample*2])
		return out, nil
	}
}
func pcmFixtureAdapter(t *testing.T, req Request) (PCMDecoder, *int, *string) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(req.Root, "track.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	calls := new(int)
	version := new(string)
	*version = "reference/1"
	return pcmAdapter(t, source, pcmBytes(), calls, version), calls, version
}
func noPCM(t *testing.T, r Result, err error) {
	t.Helper()
	noPacketPayload(t, r, err)
	if r.MP4PCM != nil {
		t.Fatal("failed operation exposed decoded metadata")
	}
}

func TestPCMUnitsAndWAVContinuationsBindCompleteOutput(t *testing.T) {
	req := pcmRequest(t)
	adapter, calls, _ := pcmFixtureAdapter(t, req)
	req.UnitsOnly = true
	units, err := GetWithDecoder(context.Background(), req, adapter)
	if err != nil || units.Status != "units_available" || units.MP4PCM == nil || units.Resource != nil || units.MP4 != nil || len(units.Units) != 1 || units.MP4PCM.TotalSamples != "1500" || units.MP4PCM.OutputRate != 1000 || units.MP4PCM.OutputChannels != 1 || units.MP4PCM.ProbedChannels != 2 || units.MP4PCM.FullPCMSHA256 != digest(pcmBytes()) {
		t.Fatal(units, err)
	}
	if units.MP4PCM.SourceClock.EncodedMediaTicks != "5120" || units.MP4PCM.SourceClock.MediaTimescale != 44100 || units.MP4PCM.PresentationMapping != "not_established" {
		t.Fatal("source and output clocks conflated", units.MP4PCM)
	}
	req.UnitsOnly = false
	req.MaxBytes = 1044
	zero := int64(0)
	req.PCMSample = &zero
	var delivered []byte
	next := int64(0)
	for range 3 {
		r, err := GetWithDecoder(context.Background(), req, adapter)
		if err != nil || r.Resource == nil || r.MP4PCM == nil || r.MP4PCM.Range == nil || r.Locator != nil || r.RepresentationID != units.RepresentationID {
			t.Fatal(r, err)
		}
		a := r.MP4PCM
		wave := r.Resource.Data
		if len(wave) != 1044 || string(wave[:4]) != "RIFF" || string(wave[8:16]) != "WAVEfmt " || binary.LittleEndian.Uint32(wave[4:]) != uint32(len(wave)-8) || binary.LittleEndian.Uint16(wave[20:]) != 1 || binary.LittleEndian.Uint16(wave[22:]) != 1 || binary.LittleEndian.Uint32(wave[24:]) != 1000 || binary.LittleEndian.Uint16(wave[32:]) != 2 || binary.LittleEndian.Uint16(wave[34:]) != 16 || string(wave[36:40]) != "data" || binary.LittleEndian.Uint32(wave[40:]) != 1000 {
			t.Fatal("invalid bounded PCM WAV", wave[:44])
		}
		if r.Resource.SHA256 != digest(wave) || a.RangePCMSHA256 != digest(wave[44:]) || a.FullPCMSHA256 != units.MP4PCM.FullPCMSHA256 || a.Range.StartSample != strconv.FormatInt(next, 10) || a.Range.EndSample != strconv.FormatInt(next+500, 10) || a.Range.OffsetUnit != "decoder_output_samples" {
			t.Fatal("range identity or sample bounds", a, r.Resource)
		}
		next += 500
		delivered = append(delivered, wave[44:]...)
		if r.EndOfSource {
			if next != 1500 || r.Continuation != "" || r.Status != "complete_range" {
				t.Fatal("end", r)
			}
		} else if r.Status != "partial_range" || r.Continuation == "" {
			t.Fatal("continuation missing", r)
		}
		req.Cursor, req.Representation, req.PCMSample = r.Continuation, "", nil
	}
	if *calls != 4 || !bytes.Equal(delivered, pcmBytes()) {
		t.Fatal("complete decoded stream did not tile", *calls, len(delivered))
	}
	b, err := json.Marshal(units)
	if err != nil || !strings.Contains(string(b), `"total_samples":"1500"`) {
		t.Fatal(string(b), err)
	}
}

func TestPCMRequiresPerCallAuthorityAndRejectsExplicitZeroMisuse(t *testing.T) {
	base := pcmRequest(t)
	adapter, calls, _ := pcmFixtureAdapter(t, base)
	base.MaxBytes = 100
	first, err := GetWithDecoder(context.Background(), base, adapter)
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	for _, tc := range []struct {
		name string
		edit func(*Request)
	}{
		{"no_permission", func(r *Request) { r.Decode = false }},
		{"cursor_no_permission", func(r *Request) { r.Decode = false; r.Cursor = first.Continuation; r.Representation = "" }},
		{"wrong_rep_zero", func(r *Request) { r.Representation = MP4PacketRepresentation; r.PCMSample = &zero }},
		{"units_zero", func(r *Request) { r.UnitsOnly = true; r.PCMSample = &zero }},
		{"cursor_zero", func(r *Request) { r.Cursor = first.Continuation; r.PCMSample = &zero }},
		{"header_only", func(r *Request) { r.MaxBytes = 44 }},
		{"encoded_sample", func(r *Request) { r.Sample = 1 }},
		{"source_unit", func(r *Request) { r.Unit = 1 }},
		{"decode_default_text", func(r *Request) { r.Representation = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.edit(&req)
			before := *calls
			r, err := GetWithDecoder(context.Background(), req, adapter)
			noPCM(t, r, err)
			if *calls != before {
				t.Fatal("invalid request invoked decoder")
			}
		})
	}
	base.PCMSample = &zero
	if r, err := GetWithDecoder(context.Background(), base, adapter); err != nil || r.MP4PCM.Range.StartSample != "0" {
		t.Fatal("explicit zero rejected", r, err)
	}
}

func TestPCMContinuationsRefuseChangedWholeOutputAndForgedReferences(t *testing.T) {
	req := pcmRequest(t)
	req.MaxBytes = 100
	adapter, _, version := pcmFixtureAdapter(t, req)
	first, err := GetWithDecoder(context.Background(), req, adapter)
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := parsePCMCursor(first.Continuation)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*pcmCursor)
	}{{"asset", func(c *pcmCursor) { c.AssetID = "sha256:" + strings.Repeat("0", 64) }}, {"extractor", func(c *pcmCursor) { c.Extractor = "another/1" }}, {"identity", func(c *pcmCursor) { c.RepresentationID = "sha256:" + strings.Repeat("0", 64) }}, {"past_end", func(c *pcmCursor) { c.NextSample = "1500" }}, {"noncanonical", func(c *pcmCursor) { c.NextSample = "01" }}, {"negative", func(c *pcmCursor) { c.NextSample = "-1" }}, {"huge", func(c *pcmCursor) { c.NextSample = "4294967296" }}} {
		t.Run(tc.name, func(t *testing.T) {
			c := original
			tc.edit(&c)
			r := req
			r.Cursor = encodePCMCursor(c)
			out, err := GetWithDecoder(context.Background(), r, adapter)
			noPCM(t, out, err)
		})
	}
	req.Cursor = first.Continuation
	*version = "reference/2"
	out, err := GetWithDecoder(context.Background(), req, adapter)
	noPCM(t, out, err)
	if out.Status != "stale_reference" {
		t.Fatal(out.Status, err)
	}
	source, err := os.ReadFile(filepath.Join(req.Root, "track.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	changed := pcmBytes()
	changed[len(changed)-1] ^= 255
	calls := 0
	*version = "reference/1"
	// A change outside the selected slice still changes complete-output identity.
	out, err = GetWithDecoder(context.Background(), req, pcmAdapter(t, source, changed, &calls, version))
	noPCM(t, out, err)
	if out.Status != "stale_reference" {
		t.Fatal(out.Status, err)
	}
}

func TestPCMFailureAndPrivateRevalidationNeverExposeCollectedBytes(t *testing.T) {
	for _, name := range []string{"late_error", "cancelled", "budget", "unavailable", "mutated_private", "replaced_private", "bad_measurement"} {
		t.Run(name, func(t *testing.T) {
			req := pcmRequest(t)
			adapter, _, _ := pcmFixtureAdapter(t, req)
			before, err := os.ReadFile(filepath.Join(req.Root, "track.mp3"))
			if err != nil {
				t.Fatal(err)
			}
			var deniedReplacement error
			failure := func(ctx context.Context, path, format string, size int64, q decode.PCMRequest) (decode.PCMRange, error) {
				out, err := adapter(ctx, path, format, size, q)
				if err != nil {
					return out, err
				}
				switch name {
				case "late_error":
					return out, decode.ErrPCMDecode
				case "cancelled":
					return out, context.Canceled
				case "budget":
					return out, decode.ErrPCMBudget
				case "unavailable":
					return out, decode.ErrPCMUnavailable
				case "mutated_private":
					b := bytes.Clone(before)
					b[len(b)-1] ^= 255
					if err := os.WriteFile(path, b, 0600); err != nil {
						t.Fatal(err)
					}
				case "replaced_private":
					if err := os.Rename(path, path+".old"); err != nil {
						// Native Windows retains a handle that denies replacement.
						// Accept only that exact denial, prove the private bytes
						// stayed intact, and still discard the adapter's payload.
						if runtime.GOOS != "windows" || !errors.Is(err, syscall.Errno(32)) {
							t.Fatal(err)
						}
						private, readErr := os.ReadFile(path)
						if readErr != nil || !bytes.Equal(private, before) {
							t.Fatal("denied replacement changed private bytes", readErr)
						}
						deniedReplacement = err
						return out, err
					}
					if err := os.WriteFile(path, before, 0600); err != nil {
						t.Fatal(err)
					}
				case "bad_measurement":
					out.FullSHA256 = ""
				}
				return out, nil
			}
			out, err := GetWithDecoder(context.Background(), req, failure)
			noPCM(t, out, err)
			want := map[string]string{"late_error": "unsupported_extraction", "cancelled": "cancelled", "budget": "budget_exceeded", "unavailable": "unavailable", "mutated_private": "changed_source", "replaced_private": "changed_source", "bad_measurement": "malformed_content"}[name]
			if deniedReplacement != nil {
				want = "unsupported_extraction"
				if !errors.Is(err, deniedReplacement) {
					t.Fatal("native replacement denial was lost", err)
				}
				t.Log("Windows denied the open-file replacement; private bytes unchanged and no payload delivered")
			}
			if out.Status != want {
				t.Fatal(out.Status, want, err)
			}
			after, err := os.ReadFile(filepath.Join(req.Root, "track.mp3"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("original changed", err)
			}
		})
	}
}

func TestPCMByteLimitsAndDefaultMissingTool(t *testing.T) {
	req := pcmRequest(t)
	adapter, _, _ := pcmFixtureAdapter(t, req)
	req.MaxBytes = 47
	out, err := GetWithDecoder(context.Background(), req, adapter)
	if err != nil || len(out.Resource.Data) != 46 || out.MP4PCM.Range.EndSample != "1" {
		t.Fatal(out, err)
	}
	saved := decode.LookPath
	t.Cleanup(func() { decode.LookPath = saved })
	decode.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	out, err = Get(context.Background(), req)
	noPCM(t, out, err)
	if out.Status != "unavailable" || !errors.Is(err, decode.ErrPCMUnavailable) {
		t.Fatal(out.Status, err)
	}
}
