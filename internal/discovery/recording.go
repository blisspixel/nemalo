package discovery

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Recording is one declared set of MP3 tracks in an Archive item, such as a
// LibriVox reading in one bitrate. Track order, titles, and durations are
// source declarations; they are not checked against the audio.
type Recording struct {
	Format string           `json:"source_format"`
	Tracks []RecordingTrack `json:"tracks"`
	Bytes  int64            `json:"declared_bytes"`
	// OrderSource is "declared_track" when every track carries a distinct
	// declared track number (directly or through its original), otherwise
	// "file_name": the source declared no usable order.
	OrderSource string   `json:"order_source"`
	Acquirable  bool     `json:"acquirable"`
	Findings    []string `json:"findings"`
}

// RecordingTrack is one file of a recording in play order. Position is the
// 1-based place in the set; DeclaredTrack is the source's own number, if any.
type RecordingTrack struct {
	Position        int     `json:"position"`
	DeclaredTrack   int     `json:"declared_track,omitempty"`
	File            string  `json:"file"`
	Title           string  `json:"title,omitempty"`
	Bytes           int64   `json:"declared_bytes"`
	DeclaredSeconds float64 `json:"declared_seconds,omitempty"`
}

const (
	maxRecordingTracks = 1000
	maxTrackBytes      = 256 << 20
)

// recordings groups the MP3 files of one item by declared Archive format.
// A derivative takes its order from the original it names. Any finding that
// leaves a track without a size, checksum, or public access, or a set larger
// than the bounds, makes the recording not acquirable as a whole.
func recordings(files []archiveFile, offered []OfferedFile) []Recording {
	byName := map[string]int{}
	for i, f := range files {
		byName[f.Name] = i
	}
	groups := map[string][]int{}
	for i, f := range files {
		if strings.EqualFold(path.Ext(f.Name), ".mp3") && f.Format != "" {
			groups[f.Format] = append(groups[f.Format], i)
		}
	}
	formats := make([]string, 0, len(groups))
	for format := range groups {
		formats = append(formats, format)
	}
	sort.Strings(formats)
	out := []Recording{}
	for _, format := range formats {
		members := groups[format]
		r := Recording{Format: textField(format, 200), Tracks: []RecordingTrack{}, OrderSource: "declared_track", Acquirable: true, Findings: []string{}}
		type ordered struct {
			index int
			track int
		}
		order := make([]ordered, 0, len(members))
		seen := map[int]bool{}
		for _, i := range members {
			track := declaredTrack(files[i].Track)
			if track == 0 {
				if original, ok := byName[stringField(files[i].Original)]; ok && original != i {
					track = declaredTrack(files[original].Track)
				}
			}
			if track == 0 || seen[track] {
				r.OrderSource = "file_name"
			}
			seen[track] = true
			order = append(order, ordered{i, track})
		}
		sort.SliceStable(order, func(a, b int) bool {
			if r.OrderSource == "declared_track" && order[a].track != order[b].track {
				return order[a].track < order[b].track
			}
			return files[order[a].index].Name < files[order[b].index].Name
		})
		if r.OrderSource == "file_name" {
			r.Findings = append(r.Findings, "no distinct declared track numbers; tracks are ordered by file name")
		}
		if len(order) > maxRecordingTracks {
			r.Acquirable = false
			r.Findings = append(r.Findings, fmt.Sprintf("more than %d tracks", maxRecordingTracks))
			order = order[:maxRecordingTracks]
		}
		for position, o := range order {
			f, offer := files[o.index], offered[o.index]
			t := RecordingTrack{Position: position + 1, File: f.Name, Title: textField(stringField(f.Title), 500), DeclaredSeconds: declaredSeconds(stringField(f.Length))}
			if r.OrderSource == "declared_track" {
				t.DeclaredTrack = o.track
			}
			switch {
			case offer.Access != "public_file_candidate" || offer.DownloadURL == "":
				r.Acquirable = false
				r.Findings = append(r.Findings, fmt.Sprintf("track %d is not a public file candidate", t.Position))
			case offer.Bytes == nil || *offer.Bytes < 1 || *offer.Bytes > maxTrackBytes:
				r.Acquirable = false
				r.Findings = append(r.Findings, fmt.Sprintf("track %d has no usable declared size", t.Position))
			case offer.MD5 == "" && offer.SHA1 == "":
				r.Acquirable = false
				r.Findings = append(r.Findings, fmt.Sprintf("track %d has no source checksum", t.Position))
			}
			if offer.Bytes != nil {
				t.Bytes = *offer.Bytes
				r.Bytes += t.Bytes
			}
			r.Tracks = append(r.Tracks, t)
		}
		if len(r.Findings) > 20 {
			r.Findings = append(r.Findings[:20], fmt.Sprintf("%d more findings", len(r.Findings)-20))
		}
		out = append(out, r)
	}
	// Raw formats that clean to the same text cannot be selected exactly.
	counts := map[string]int{}
	for _, r := range out {
		counts[r.Format]++
	}
	for i := range out {
		if counts[out[i].Format] > 1 {
			out[i].Acquirable = false
			out[i].Findings = append(out[i].Findings, "another recording in this item has the same format name")
		}
	}
	return out
}

// declaredTrack reads "7", "07", or "7/12". Anything else, or a number
// outside 1..9999, is no declared track.
func declaredTrack(raw json.RawMessage) int {
	s, _, _ := strings.Cut(stringField(raw), "/")
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 9999 {
		return 0
	}
	return n
}

// declaredSeconds reads "507.1", "08:26", or "1:02:03"; anything else is 0.
func declaredSeconds(s string) float64 {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) > 3 || s == "" {
		return 0
	}
	total := 0.0
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 || v != v || (i > 0 && v >= 60) {
			return 0
		}
		total = total*60 + v
	}
	if total > 1e6 {
		return 0
	}
	return total
}

// stringField returns a JSON string value, or "" for any other JSON type.
func stringField(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// textField keeps a declared string printable, single-line, and bounded.
func textField(s string, limit int) string {
	s = strings.Join(strings.FieldsFunc(strings.ToValidUTF8(s, ""), func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
	if len(s) > limit {
		s = strings.ToValidUTF8(s[:limit], "")
	}
	return strings.TrimSpace(s)
}
