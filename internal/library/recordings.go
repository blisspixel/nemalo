package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/blisspixel/nemalo/internal/audio"
	"github.com/blisspixel/nemalo/internal/safeio"
)

// RecordingManifestVersion names the manifest schema. A change to its fields or
// their meaning needs a new version.
const RecordingManifestVersion = "recording-manifest/1"

// RecordingManifest is one declared recording as held in a managed library:
// its tracks in declared order, the exact asset of each, and what is missing or
// conflicting. Order, titles, and declared durations come from the source;
// probed timing comes from indexing the stored bytes, without decoding.
type RecordingManifest struct {
	Schema         string `json:"schema"`
	SourceID       string `json:"source_id"`
	Format         string `json:"source_format"`
	DeclaredTracks int    `json:"declared_tracks"`
	HeldTracks     int    `json:"held_tracks"`
	// Complete means every declared position is held by exactly one asset and
	// the declared track count agrees. It is not a check of the audio content.
	Complete bool             `json:"complete"`
	Missing  []int            `json:"missing_positions"`
	Tracks   []RecordingEntry `json:"tracks"`
	// Narrators are not declared in a form Nemalo reads; the field stays empty
	// until a source supplies them, and different narrations stay distinct by
	// source and format.
	Narrators []string `json:"narrators"`
	// ProbedMs is the sum of probed encoded durations when every held track was
	// probed and the recording is complete; otherwise it is absent.
	ProbedMs *int64   `json:"probed_duration_ms,omitempty"`
	Probed   bool     `json:"probed"`
	Findings []string `json:"findings"`
}

// RecordingEntry is one held track. Start is its probed offset from the start
// of the recording, present only when every earlier track was probed.
type RecordingEntry struct {
	Position          int             `json:"position"`
	AssetID           string          `json:"asset_id"`
	Bytes             int64           `json:"bytes"`
	SourceFile        string          `json:"source_file"`
	Title             string          `json:"declared_title,omitempty"`
	DeclaredSeconds   float64         `json:"declared_seconds,omitempty"`
	Status            string          `json:"status"`
	Probe             string          `json:"probe"`
	SampleRate        int             `json:"sample_rate,omitempty"`
	Channels          int             `json:"channels,omitempty"`
	EncodedSamples    int64           `json:"encoded_samples,omitempty"`
	EncodedDurationMs int64           `json:"encoded_duration_ms,omitempty"`
	StartMs           *int64          `json:"start_ms,omitempty"`
	Chapters          []audio.Chapter `json:"chapters,omitempty"`
	Detail            string          `json:"detail,omitempty"`
}

// RecordingsResult lists the recordings held in a library.
type RecordingsResult struct {
	SchemaVersion int                 `json:"schema_version"`
	LibraryID     string              `json:"library_id"`
	Library       string              `json:"library"`
	Recordings    []RecordingManifest `json:"recordings"`
	Limitations   []string            `json:"limitations"`
}

// maxProbedRecordingBytes bounds how much stored audio one call indexes, and
// maxProbedTrackBytes how much of it one track may hold in memory.
const (
	maxProbedRecordingBytes = 4 << 30
	maxProbedTrackBytes     = 256 << 20
)

// Recordings groups current holdings by declared recording (source ID and
// format). With source set, only that source's recordings are returned and
// each held track's stored bytes are verified against its holding and indexed
// for timing and chapters. Without it, recordings are listed from the journal
// alone. Nothing is written.
func Recordings(ctx context.Context, directory, source string) (RecordingsResult, error) {
	r := RecordingsResult{SchemaVersion: 1, Recordings: []RecordingManifest{}, Limitations: []string{
		"Track order, titles, and declared durations are source declarations. Complete means every declared position is held, not that the audio was checked.",
		"Probed timing indexes MPEG frames without decoding. Narrator identity is not declared in a form Nemalo reads.",
		"Listing a recording is not evidence that anyone listened to it.",
	}}
	if len(source) > 1000 {
		return r, errors.New("source filter is at most 1000 bytes")
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return r, err
	}
	r.Library = root
	state, err := LibraryStatus(ctx, root)
	if err != nil {
		return r, err
	}
	if state.Status != "initialized" {
		return r, errors.New("recordings require an initialized library; run library init on this explicit directory")
	}
	r.LibraryID = state.LibraryID
	control, _, err := openControl(root, false)
	if err != nil {
		return r, err
	}
	defer control.close()
	_, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return r, err
	}
	type key struct{ source, format string }
	type claim struct {
		h   Holding
		src Source
	}
	groups := map[key][]claim{}
	for _, h := range latestList(lines) {
		for _, s := range h.Sources {
			if s.Recording == nil || s.SourceID == "" || (source != "" && s.SourceID != source) {
				continue
			}
			k := key{s.SourceID, s.Recording.Format}
			groups[k] = append(groups[k], claim{h, s})
		}
	}
	keys := make([]key, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].source != keys[j].source {
			return keys[i].source < keys[j].source
		}
		return keys[i].format < keys[j].format
	})
	var store *os.Root
	if source != "" {
		if store, err = safeio.OpenRoot(root); err != nil {
			return r, err
		}
		defer store.Close()
	}
	budget := int64(maxProbedRecordingBytes)
	for _, k := range keys {
		m := RecordingManifest{Schema: RecordingManifestVersion, SourceID: k.source, Format: k.format, Missing: []int{}, Tracks: []RecordingEntry{}, Narrators: []string{}, Findings: []string{}}
		counts, held := map[int]bool{}, map[string]Holding{}
		byPosition := map[int][]claim{}
		for _, c := range groups[k] {
			held[c.h.ID] = c.h
			counts[c.src.Recording.Tracks] = true
			m.DeclaredTracks = max(m.DeclaredTracks, c.src.Recording.Tracks)
			byPosition[c.src.Recording.Position] = append(byPosition[c.src.Recording.Position], c)
		}
		if len(counts) > 1 {
			m.Findings = append(m.Findings, "held tracks declare different track counts")
		}
		conflict := false
		for position := 1; position <= m.DeclaredTracks; position++ {
			claims := byPosition[position]
			if len(claims) == 0 {
				m.Missing = append(m.Missing, position)
				continue
			}
			assets := map[string]bool{}
			for _, c := range claims {
				if assets[c.h.ID] {
					continue
				}
				assets[c.h.ID] = true
				m.Tracks = append(m.Tracks, RecordingEntry{Position: position, AssetID: c.h.ID, Bytes: c.h.Bytes, SourceFile: c.src.SourceFile, Title: c.src.Recording.Title, DeclaredSeconds: c.src.Recording.DeclaredSeconds, Status: c.h.Status, Probe: "not_requested"})
			}
			if len(assets) > 1 {
				conflict = true
				m.Findings = append(m.Findings, fmt.Sprintf("position %d is claimed by %d different assets", position, len(assets)))
			}
		}
		m.HeldTracks = m.DeclaredTracks - len(m.Missing)
		m.Complete = len(m.Missing) == 0 && !conflict && len(counts) == 1 && m.DeclaredTracks > 0
		if store != nil {
			m.Probed = true
			var elapsed int64
			ordered, next := !conflict, 1
			for i := range m.Tracks {
				if err := ctx.Err(); err != nil {
					return r, err
				}
				t := &m.Tracks[i]
				// Offsets hold only through an unbroken run from position 1.
				if t.Position != next {
					ordered = false
				}
				next = t.Position + 1
				if t.Bytes > budget || t.Bytes > maxProbedTrackBytes {
					t.Probe, t.Detail = "budget_exceeded", "the call's audio indexing budget is spent"
					ordered = false
					continue
				}
				budget -= t.Bytes
				track, probeErr := probeHolding(ctx, store, held[t.AssetID])
				switch {
				case errors.Is(probeErr, errStoredObject):
					t.Probe, t.Detail = "stored_bytes_changed", "stored bytes are missing or differ from the holding; run library audit"
				case probeErr != nil:
					t.Probe, t.Detail = "failed", probeErr.Error()
				default:
					t.Probe = "indexed"
					t.SampleRate, t.Channels, t.EncodedSamples, t.EncodedDurationMs, t.Chapters = track.SampleRate, track.Channels, track.EncodedSamples, track.EncodedDurationMs, track.Chapters
				}
				if t.Probe != "indexed" {
					ordered = false
				}
				if ordered {
					start := elapsed
					t.StartMs = &start
					elapsed += t.EncodedDurationMs
				}
			}
			if ordered && m.Complete {
				m.ProbedMs = &elapsed
			}
		}
		r.Recordings = append(r.Recordings, m)
	}
	return r, nil
}

// probeHolding reads one stored asset, verifies its size and SHA-256 against
// the holding, and indexes it. The verified bytes are the bytes indexed.
func probeHolding(ctx context.Context, store *os.Root, h Holding) (audio.Track, error) {
	name, err := assetOSPath(h.Path)
	if err != nil {
		return audio.Track{}, err
	}
	info, err := store.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() != h.Bytes {
		return audio.Track{}, errStoredObject
	}
	f, err := safeio.OpenRegular(store, name, info, os.O_RDONLY)
	if err != nil {
		return audio.Track{}, errStoredObject
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(importReader{ctx, f}, h.Bytes+1))
	if err != nil {
		return audio.Track{}, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != h.Bytes || hex.EncodeToString(sum[:]) != h.SHA256 {
		return audio.Track{}, errStoredObject
	}
	return audio.Probe(ctx, data)
}
