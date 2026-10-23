package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/audio"
)

func packetRequest(t *testing.T, n int) (Request, []byte) {
	t.Helper()
	data := mp4Audio(n)
	start := bytes.Index(data, []byte("mdat")) + 4
	for p := range n {
		for j := range 200 {
			data[start+p*200+j] = byte(p + 1)
		}
	}
	req := audioFixture(t, data)
	req.Representation = MP4PacketRepresentation
	return req, data
}

func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

func noPacketPayload(t *testing.T, r Result, err error) {
	t.Helper()
	if err == nil || r.MP4Packets != nil || r.Resource != nil || r.Locator != nil || r.Continuation != "" || r.Coverage != nil || r.Units != nil || r.EndOfSource || r.EndOfUnit {
		t.Fatalf("failed request exposed a result: %+v, %v", r, err)
	}
}

func TestMP4PacketsTileOriginalBytesWithBoundContinuations(t *testing.T) {
	req, data := packetRequest(t, 270)
	req.UnitsOnly = true
	units, err := Get(context.Background(), req)
	entryStart := bytes.Index(data, []byte("mp4a")) - 4
	entryEnd := entryStart + int(binary.BigEndian.Uint32(data[entryStart:]))
	if err != nil || units.Status != "units_available" || units.MP4 != nil || units.Resource != nil || len(units.Units) != 1 || units.MP4Packets == nil {
		t.Fatal(units, err)
	}
	init := units.MP4Packets.Initialization
	if !bytes.Equal(init.Data, data[entryStart:entryEnd]) || init.SHA256 != digest(init.Data) || init.STSDVersion != 0 || init.Encoding != "base64" {
		t.Fatal("initialization is not the original entry", init)
	}
	if units.MP4Packets.Presentation != "not_applied" || units.MP4Packets.PCMEquivalence != "not_established" {
		t.Fatal("unverified timing implied", units.MP4Packets)
	}
	req.UnitsOnly, req.MaxBytes = false, 65536
	var delivered []byte
	next, calls := 0, 0
	media := bytes.Index(data, []byte("mdat")) + 4
	for {
		got, err := Get(context.Background(), req)
		if err != nil || got.MP4Packets == nil || got.Resource == nil || got.Locator == nil {
			t.Fatal(got, err)
		}
		a := got.MP4Packets
		if got.RepresentationID != units.RepresentationID || got.MP4 != nil || a.Initialization.SHA256 != init.SHA256 || got.Locator.Start != next || got.Locator.End != a.NextPacket || got.Locator.OffsetUnit != "mp4_packets" || a.PacketCount != 270 || a.Timescale != 44100 || a.Codec != "aac" {
			t.Fatal("range identity/timeline", got)
		}
		pos := 0
		for _, p := range a.Packets {
			if p.Index != next || p.ByteStart != int64(media+next*200) || p.ByteEnd != p.ByteStart+200 || p.DecodeStart != strconv.Itoa(next*1024) || p.DecodeEnd != strconv.Itoa((next+1)*1024) || !bytes.Equal(got.Resource.Data[pos:pos+200], data[p.ByteStart:p.ByteEnd]) {
				t.Fatal("wrong original packet", p, next)
			}
			next++
			pos += 200
		}
		if pos != len(got.Resource.Data) || got.Resource.TotalBytes != pos || got.Resource.SHA256 != digest(got.Resource.Data) || got.Resource.SHA256 == req.AssetID[7:] || len(a.Packets) > 128 {
			t.Fatal("payload binding/bound", got.Resource)
		}
		delivered = append(delivered, got.Resource.Data...)
		calls++
		if a.EndOfTrack {
			if !got.EndOfSource || got.Status != "complete_range" || got.Continuation != "" || next != 270 {
				t.Fatal("track end", got)
			}
			break
		}
		if got.Status != "partial_range" || got.Continuation == "" || a.Stop != "packet_limit" || len(a.Packets) != 128 || calls > 3 {
			t.Fatal("packet stop", got)
		}
		req.Cursor, req.Representation = got.Continuation, ""
	}
	if calls != 3 || !bytes.Equal(delivered, data[media:]) {
		t.Fatal("packets did not tile source", calls, len(delivered))
	}
	before := data
	after, err := os.ReadFile(filepath.Join(req.Root, "track.mp3"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("source changed", err)
	}
	entries, _ := os.ReadDir(req.Root)
	if len(entries) != 1 {
		t.Fatal("content wrote library state", entries)
	}
}

func TestMP4PacketByteLimitsAndExplicitSelections(t *testing.T) {
	req, data := packetRequest(t, 10)
	req.MaxBytes = 399
	r, err := Get(context.Background(), req)
	if err != nil || r.MP4Packets.NextPacket != 1 || r.MP4Packets.Stop != "byte_limit" || len(r.Resource.Data) != 200 {
		t.Fatal(r, err)
	}
	first := 7
	req.Packet, req.MaxBytes = &first, 600
	r, err = Get(context.Background(), req)
	if err != nil || r.Locator.Start != 7 || r.Locator.End != 10 || !r.EndOfSource || !bytes.Equal(r.Resource.Data, data[len(data)-600:]) {
		t.Fatal(r, err)
	}
	again, err := Get(context.Background(), req)
	if err != nil || !bytes.Equal(again.Resource.Data, r.Resource.Data) || again.RepresentationID != r.RepresentationID {
		t.Fatal("read was not stateless", err)
	}
	req.MaxBytes = 199
	r, err = Get(context.Background(), req)
	noPacketPayload(t, r, err)
	if r.Status != "invalid_request" || !strings.Contains(err.Error(), "200 bytes") {
		t.Fatal(r.Status, err)
	}
}

func TestMP4PacketSelectorsAndForgedContinuations(t *testing.T) {
	base, _ := packetRequest(t, 5)
	base.MaxBytes = 200
	first, err := Get(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	original, err := parseCursor(first.Continuation)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*cursor)
	}{
		{"asset", func(c *cursor) { c.AssetID = "sha256:" + strings.Repeat("0", 64) }},
		{"extractor", func(c *cursor) { c.Extractor = audio.MP4Extractor }},
		{"representation", func(c *cursor) { c.Representation = "mpeg-audio/1" }},
		{"identity", func(c *cursor) { c.RepresentationID = "sha256:" + strings.Repeat("0", 64) }},
		{"unit", func(c *cursor) { c.Unit = 1 }},
		{"part", func(c *cursor) { c.Part = "one" }},
		{"member", func(c *cursor) { c.Member = "encoded_packets" }},
		{"member_hash", func(c *cursor) { c.MemberSHA256 = strings.Repeat("0", 64) }},
		{"past_end", func(c *cursor) { c.Offset = 5 }},
		{"negative", func(c *cursor) { c.Offset = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := original
			tc.mutate(&c)
			req := base
			req.Cursor = encodeCursor(c)
			r, err := Get(context.Background(), req)
			noPacketPayload(t, r, err)
		})
	}
	zero, negative, large := 0, -1, 8_000_000
	for _, tc := range []struct {
		name   string
		mutate func(*Request)
	}{
		{"wrong_rep_zero", func(r *Request) { r.Packet = &zero; r.Representation = "mp4-audio/1" }},
		{"default_rep_zero", func(r *Request) { r.Packet = &zero; r.Representation = "" }},
		{"units_zero", func(r *Request) { r.Packet = &zero; r.UnitsOnly = true }},
		{"cursor_zero", func(r *Request) { r.Packet = &zero; r.Cursor = first.Continuation }},
		{"negative", func(r *Request) { r.Packet = &negative }},
		{"too_large", func(r *Request) { r.Packet = &large }},
		{"unit", func(r *Request) { r.Unit = 1 }},
		{"offset", func(r *Request) { r.Offset = 1 }},
		{"sample", func(r *Request) { r.Sample = 1 }},
		{"part", func(r *Request) { r.Part = "one" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req)
			r, err := Get(context.Background(), req)
			noPacketPayload(t, r, err)
		})
	}
	base.Packet = &zero
	if r, err := Get(context.Background(), base); err != nil || r.Locator.Start != 0 {
		t.Fatal("explicit zero refused", r, err)
	}
}

func TestMP4PacketChangedSourceAndCancellationExposeNothing(t *testing.T) {
	req, data := packetRequest(t, 5)
	path := filepath.Join(req.Root, "track.mp3")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 255
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	r, err := Get(context.Background(), req)
	noPacketPayload(t, r, err)
	if r.Status != "changed_source" {
		t.Fatal(r.Status, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err = Get(ctx, req)
	noPacketPayload(t, r, err)
	if r.Status != "cancelled" {
		t.Fatal(r.Status, err)
	}
	req = audioFixture(t, mp3Frames(10))
	req.Representation = MP4PacketRepresentation
	r, err = Get(context.Background(), req)
	noPacketPayload(t, r, err)
	if r.Status != "unsupported_extraction" {
		t.Fatal(r.Status, err)
	}
}

func TestMP4PacketTicksRemainExactBeyondJSONSafeIntegers(t *testing.T) {
	const count = 3_000_000
	data := mp4Audio(1)
	set := func(kind string, offset int, value uint32) {
		p := bytes.Index(data, []byte(kind))
		if p < 0 {
			t.Fatal(kind)
		}
		binary.BigEndian.PutUint32(data[p+offset:], value)
	}
	set("stsz", 8, 1)
	set("stsz", 12, count)
	set("stsc", 16, count)
	set("stts", 12, count)
	set("stts", 16, ^uint32(0))
	set("mdhd", 16, 1000)
	media := bytes.Index(data, []byte("mdat"))
	data = append(data[:media+4], make([]byte, count)...)
	binary.BigEndian.PutUint32(data[media-4:], count+8)
	req := audioFixture(t, data)
	req.Representation = MP4PacketRepresentation
	first := count - 2
	req.Packet = &first
	r, err := Get(context.Background(), req)
	if err != nil || r.MP4Packets == nil || len(r.MP4Packets.Packets) != 2 {
		t.Fatal(r.Status, err)
	}
	want := strconv.FormatInt(int64(first)*int64(^uint32(0)), 10)
	if r.MP4 != nil || r.MP4Packets.Packets[0].DecodeStart != want {
		t.Fatal("tick precision lost", r.MP4Packets)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	packets := wire["mp4_packets"].(map[string]any)["packets"].([]any)
	if packets[0].(map[string]any)["decode_start"] != want {
		t.Fatal("ticks were not decimal strings", string(encoded))
	}
}

// Extend the original sample entry or leave unaccounted stsd tail bytes. Every
// affected ancestor length and the declared physical media offset is adjusted.
func extendPacketDescription(t *testing.T, data []byte, n int, inside bool) []byte {
	t.Helper()
	entry := bytes.Index(data, []byte("mp4a")) - 4
	end := entry + int(binary.BigEndian.Uint32(data[entry:]))
	extra := make([]byte, n)
	binary.BigEndian.PutUint32(extra, uint32(n))
	copy(extra[4:], "free")
	out := append(bytes.Clone(data[:end]), extra...)
	out = append(out, data[end:]...)
	for _, kind := range []string{"moov", "trak", "mdia", "minf", "stbl", "stsd"} {
		p := bytes.Index(out, []byte(kind)) - 4
		binary.BigEndian.PutUint32(out[p:], binary.BigEndian.Uint32(out[p:])+uint32(n))
	}
	if inside {
		binary.BigEndian.PutUint32(out[entry:], uint32(end-entry+n))
	}
	p := bytes.Index(out, []byte("stco")) + 12
	binary.BigEndian.PutUint32(out[p:], binary.BigEndian.Uint32(out[p:])+uint32(n))
	return out
}

func TestMP4PacketInitializationRefusesOversizeAndUnaccountedTail(t *testing.T) {
	for _, tc := range []struct {
		name   string
		n      int
		inside bool
		status string
	}{{"oversized", 16 << 10, true, "budget_exceeded"}, {"trailing", 8, false, "unsupported_extraction"}} {
		t.Run(tc.name, func(t *testing.T) {
			data := extendPacketDescription(t, mp4Audio(4), tc.n, tc.inside)
			req := audioFixture(t, data)
			req.Representation = "mp4-audio/1"
			req.UnitsOnly = true
			if r, err := Get(context.Background(), req); err != nil || r.MP4 == nil {
				t.Fatal("legacy representation changed", r.Status, err)
			}
			req.Representation = MP4PacketRepresentation
			r, err := Get(context.Background(), req)
			noPacketPayload(t, r, err)
			if r.Status != tc.status {
				t.Fatal(r.Status, err)
			}
		})
	}
}

func TestMP4PacketEmptyBoundaryCannotBeSkippedByContinuation(t *testing.T) {
	data := mp4Audio(3)
	p := bytes.Index(data, []byte("stsz"))
	sizes := binary.BigEndian.AppendUint32(nil, 200)
	sizes = binary.BigEndian.AppendUint32(sizes, 0)
	sizes = binary.BigEndian.AppendUint32(sizes, 200)
	data = append(append(bytes.Clone(data[:p+16]), sizes...), data[p+16:]...)
	binary.BigEndian.PutUint32(data[p+8:], 0)
	for _, kind := range []string{"moov", "trak", "mdia", "minf", "stbl", "stsz"} {
		start := bytes.Index(data, []byte(kind)) - 4
		binary.BigEndian.PutUint32(data[start:], binary.BigEndian.Uint32(data[start:])+12)
	}
	offset := bytes.Index(data, []byte("stco")) + 12
	binary.BigEndian.PutUint32(data[offset:], binary.BigEndian.Uint32(data[offset:])+12)
	media := bytes.Index(data, []byte("mdat")) - 4
	binary.BigEndian.PutUint32(data[media:], 408)
	data = data[:len(data)-200]
	req := audioFixture(t, data)
	req.Representation = MP4PacketRepresentation
	r, err := Get(context.Background(), req)
	if err != nil || r.MP4Packets == nil || r.Resource == nil || r.MP4Packets.Stop != "unsupported_empty_packet" || r.MP4Packets.NextPacket != 1 || r.EndOfSource || r.Continuation == "" || len(r.Coverage.KnownGaps) != 1 || r.Coverage.KnownGaps[0] != "empty_packet_at_1" {
		t.Fatal("empty boundary was skipped or hidden", r, err)
	}
	token := r.Continuation
	req.Cursor = token
	r, err = Get(context.Background(), req)
	noPacketPayload(t, r, err)
	if r.Status != "unsupported_extraction" {
		t.Fatal("empty continuation became progress", r.Status, err)
	}
	req.Cursor = ""
	later := 2
	req.Packet = &later
	r, err = Get(context.Background(), req)
	if err != nil || r.Locator.Start != 2 || r.Locator.End != 3 || !r.EndOfSource || len(r.Resource.Data) != 200 {
		t.Fatal("explicit later selection failed", r, err)
	}
}
