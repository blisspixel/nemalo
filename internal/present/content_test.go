package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/audio"
	"github.com/blisspixel/nemalo/internal/content"
)

func TestContentEscapesSourceWithoutFlatteningLines(t *testing.T) {
	r := content.Result{Status: "partial_range", AssetID: "asset", Representation: "epub-structure/1", Text: "日本語\n\nunsafe\x1b[31m\r", Continuation: "token", Locator: &content.Locator{Unit: 1, Start: 2, End: 4, OffsetUnit: "utf8_bytes"}, Coverage: &content.Coverage{Text: "known_extraction_gaps", KnownGaps: []string{"fig"}, Unknown: []string{"layout"}}, Parts: []content.Part{{ID: "fig", Gap: "image_pixels_not_text"}}}
	s := Content(r)
	if strings.ContainsAny(s, "\x1b\r") || !strings.Contains(s, "日本語\n\nunsafe\\x1b[31m\\r") || !strings.Contains(s, "image_pixels_not_text") || !strings.Contains(s, "Next range available") || !strings.Contains(s, "not evidence of reading") {
		t.Fatal(s)
	}
}

func TestContentMarksLocatedGapsWithoutAlteringSource(t *testing.T) {
	alt, empty, hostile := "The old sea-dog", "", "x\x1b[2Jy"
	r := content.Result{Status: "complete_range", Representation: "epub-text/2", Text: "CHAPTER I\nProse.\nEnd.\n",
		Locator:  &content.Locator{Unit: 0, Start: 100, End: 122, OffsetUnit: "utf8_bytes"},
		Coverage: &content.Coverage{Text: "known_extraction_gaps", KnownGaps: []string{"a", "b", "c", "d", "e"}},
		Gaps: []content.Gap{
			{Part: "a", Element: "img", Reason: "image_pixels_not_text", Offset: 110, Image: &content.GapImage{Alt: &alt}},
			{Part: "b", Element: "table", Reason: "table_relationships_not_extracted", Offset: 117},
			{Part: "c", Element: "img", Reason: "image_pixels_not_text", Offset: 122, Image: &content.GapImage{}},
			{Part: "d", Element: "img", Reason: "image_pixels_not_text", Offset: 122, Image: &content.GapImage{Alt: &empty}},
			{Part: "e", Element: "img", Reason: "image_pixels_not_text", Offset: 122, Image: &content.GapImage{Alt: &hostile}},
		}}
	s := Content(r)
	want := "CHAPTER I\n[image: The old sea-dog]Prose.\n[table omitted]End.\n[image, no alt text][image, empty alt text]" + `[image: x\x1b[2Jy]` + "\n"
	if !strings.Contains(s, want) || strings.Contains(s, "\x1b") || !strings.Contains(s, "5 located gap(s)") {
		t.Fatalf("%q", s)
	}
	if r.Text != "CHAPTER I\nProse.\nEnd.\n" {
		t.Fatal("presentation mutated source text")
	}
}

func TestContentUnitsResourcesAndContinuationCommand(t *testing.T) {
	units := Content(content.Result{Status: "units_available", Units: []content.Unit{{Index: 0, Extraction: "supported_text_with_gaps", TextBytes: 12, KnownGaps: 2, Linear: "yes", Member: "a.xhtml"}, {Index: 1, Extraction: "unsupported_document", Linear: "no", Member: "b.svg"}}, UnsupportedUnits: []int{1}})
	if !strings.Contains(units, "Reading-order units: 2") || !strings.Contains(units, "supported text with gaps") || !strings.Contains(units, "Unsupported units [1]") {
		t.Fatal(units)
	}
	res := Content(content.Result{Status: "partial_range", Resource: &content.ResourceData{Member: "i.png", SHA256: "abc", TotalBytes: 10, Data: []byte("1234")}, Locator: &content.Locator{OffsetUnit: "resource_bytes"}})
	if !strings.Contains(res, "4 of 10 bytes") || strings.Contains(res, "1234") {
		t.Fatal(res)
	}
	cmd := ContentCommand(content.Result{Status: "partial_range", Text: "a", Continuation: "TOKEN", Locator: &content.Locator{}})
	if !strings.Contains(cmd, "Continue with: --cursor TOKEN") || !strings.Contains(cmd, "(utf8 bytes)") {
		t.Fatal(cmd)
	}
	if strings.Contains(ContentCommand(content.Result{Status: "complete_range", Text: "a", Locator: &content.Locator{}}), "--cursor") {
		t.Fatal("cursor hint without continuation")
	}
}

func TestContentPresentsAudioIndexAndRangeWithoutBytes(t *testing.T) {
	presented, length := int64(1000), int64(9000)
	r := content.Result{Status: "partial_range", Representation: "mpeg-audio/1",
		Audio:      &audio.Track{Format: "mpeg1_layer3", SampleRate: 44100, Channels: 2, ChannelMode: "joint_stereo", Bitrate: "variable", Frames: 3, EncodedSamples: 3456, EncodedDurationMs: 78, PresentedSamples: &presented, Declared: audio.Declared{LengthMs: &length}, Metadata: audio.Metadata{Title: "Track\x1b[2J"}, Chapters: []audio.Chapter{{ID: "c1", Title: "One", EndMs: 50}}, Gaps: []audio.Region{{Kind: "trailing_unsynchronized_bytes", Start: 1, End: 2}}, Findings: []string{"finding"}},
		AudioRange: &audio.Range{FirstFrame: 1, Frames: 2, PrerollFrames: 1, PrerollComplete: true, SampleStart: 1152, SampleEnd: 3456, SampleRate: 44100, ByteStart: 0, ByteEnd: 1251},
		Resource:   &content.ResourceData{SHA256: "abc", TotalBytes: 2000, Data: []byte("raw audio bytes")},
		Locator:    &content.Locator{OffsetUnit: "encoded_samples"}}
	s := Content(r)
	for _, want := range []string{"mpeg1_layer3, 44100 Hz", "Indexed frames: 3", "Presented samples", "ID3 declared length: 9000 ms", "Declared chapter c1: ", "(One)", "trailing unsynchronized bytes", "Frames 1..3 (1 preroll frame(s), complete: true)", "not decoded"} {
		if !strings.Contains(s, want) {
			t.Fatal("missing", want, s)
		}
	}
	if strings.Contains(s, "raw audio bytes") || strings.Contains(s, "\x1b") {
		t.Fatal("audio bytes or control characters printed", s)
	}
	r.Status, r.AudioRange, r.Resource, r.Units = "units_available", nil, nil, []content.Unit{{Kind: "mpeg_audio_track"}}
	if units := Content(r); strings.Contains(units, "Reading-order units") || !strings.Contains(units, "Indexed frames: 3") {
		t.Fatal("audio units described as EPUB reading order", units)
	}
}

func TestContentPresentsMP4PacketEvidenceWithoutRenderingPayload(t *testing.T) {
	r := content.Result{Status: "partial_range", Representation: "mp4-audio/2", Units: []content.Unit{{Index: 0, Kind: "mp4_audio_track"}}, MP4Packets: &content.MP4PacketAccess{Codec: "aac", PacketCount: 20, DeclaredRate: 44100, DeclaredChannels: 1, Timescale: 44100, Packets: []content.MP4PacketLocation{{Index: 0}}, NextPacket: 1, Stop: "byte_limit", Initialization: content.MP4Initialization{Data: []byte("SECRET_INITIALIZATION"), SHA256: "entry_hash"}}, Resource: &content.ResourceData{Data: []byte("SECRET_PACKET"), SHA256: "payload_hash", TotalBytes: 13}, Continuation: "TOKEN"}
	s := ContentCommand(r)
	for _, want := range []string{"20 source packets", "next 1", "entry_hash", "payload_hash", "not a playable MP4 container", "--json", "Continue with: --cursor TOKEN"} {
		if !strings.Contains(s, want) {
			t.Fatal("missing packet evidence", want, s)
		}
	}
	for _, hidden := range []string{"SECRET_PACKET", "SECRET_INITIALIZATION", "Reading-order units"} {
		if strings.Contains(s, hidden) {
			t.Fatal("raw bytes or wrong units shown", s)
		}
	}
	r.Locator = &content.Locator{Unit: 0, Start: 3, End: 4, OffsetUnit: "mp4_packets"}
	if s := Content(r); !strings.Contains(s, "packets 3..4 (zero-based, half-open)") || strings.Contains(s, "bytes 3..4") {
		t.Fatal("packet ordinals described as bytes", s)
	}
}
