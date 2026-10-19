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
	"slices"
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
	Schema   string `json:"schema"`
	SourceID string `json:"source_id"`
	// Format is the declared Archive format of a track set, or the file name
	// of a single-file recording such as one M4B.
	Format         string `json:"source_format"`
	Kind           string `json:"kind"`
	DeclaredTracks int    `json:"declared_tracks"`
	HeldTracks     int    `json:"held_tracks"`
	// Complete means every declared position is held by exactly one asset and
	// the declared track count agrees. It is not a check of the audio content.
	Complete bool             `json:"complete"`
	Missing  []int            `json:"missing_positions"`
	Tracks   []RecordingEntry `json:"tracks"`
	// Narrators are the distinct readers held tracks were credited with, in
	// first-credited order, from a LibriVox project that linked back to the
	// source and whose sections matched its tracks. Empty when no track was
	// credited. Different narrations stay distinct by source and format.
	Narrators []Reader `json:"narrators"`
	// Narration is the reading this recording is of, when its tracks' sources
	// name one; recordings in other formats of the same reading share it.
	Narration *Narration `json:"narration,omitempty"`
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
	Readers           []Reader        `json:"readers,omitempty"`
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
	// Narrations group the recordings that name the same reading. A
	// recording without narration evidence is in none, and is never grouped
	// by title.
	Narrations  []NarrationGroup `json:"narrations"`
	Limitations []string         `json:"limitations"`
}

// NarrationGroup is one reading and the recordings held of it, such as a
// LibriVox reading held as MP3 tracks and as one M4B.
type NarrationGroup struct {
	Narration  Narration      `json:"narration"`
	Recordings []RecordingRef `json:"recordings"`
}

// RecordingRef names one recording of a narration.
type RecordingRef struct {
	SourceID string `json:"source_id"`
	Format   string `json:"source_format"`
	Kind     string `json:"kind"`
	Complete bool   `json:"complete"`
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
	r := RecordingsResult{SchemaVersion: 1, Recordings: []RecordingManifest{}, Narrations: []NarrationGroup{}, Limitations: []string{
		"Track order, titles, and declared durations are source declarations. Complete means every declared position is held, not that the audio was checked.",
		"Probed timing indexes MPEG frames or MP4 sample tables without decoding. Narrators and narrations come only from a LibriVox project that names the Archive item back; nothing is inferred from titles.",
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
	type key struct{ source, format, kind string }
	type claim struct {
		h   Holding
		src Source
		rec *SourceRecording
	}
	groups := map[key][]claim{}
	for _, h := range latestList(lines) {
		for _, s := range h.Sources {
			if s.SourceID == "" || (source != "" && s.SourceID != source) {
				continue
			}
			switch {
			case s.Recording != nil:
				groups[key{s.SourceID, s.Recording.Format, "tracks"}] = append(groups[key{s.SourceID, s.Recording.Format, "tracks"}], claim{h, s, s.Recording})
			case h.Format == "m4b" && s.Kind == "packet" && s.SourceFile != "":
				// One M4B is a whole recording by convention: one track.
				k := key{s.SourceID, s.SourceFile, "single_file"}
				groups[k] = append(groups[k], claim{h, s, &SourceRecording{Format: s.SourceFile, Position: 1, Tracks: 1}})
			}
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
		m := RecordingManifest{Schema: RecordingManifestVersion, SourceID: k.source, Format: k.format, Kind: k.kind, Missing: []int{}, Tracks: []RecordingEntry{}, Narrators: []Reader{}, Findings: []string{}}
		counts, held := map[int]bool{}, map[string]Holding{}
		byPosition := map[int][]claim{}
		narrations := map[string]Narration{}
		unnamed := false
		for _, c := range groups[k] {
			held[c.h.ID] = c.h
			counts[c.rec.Tracks] = true
			m.DeclaredTracks = max(m.DeclaredTracks, c.rec.Tracks)
			byPosition[c.rec.Position] = append(byPosition[c.rec.Position], c)
			if c.src.Narration != nil {
				narrations[c.src.Narration.ID] = *c.src.Narration
			} else {
				unnamed = true
			}
		}
		// A recording names a reading only when every held track names it.
		switch {
		case len(narrations) > 1:
			m.Findings = append(m.Findings, "held tracks name different narrations")
		case len(narrations) == 1 && unnamed:
			m.Findings = append(m.Findings, "some held tracks name no narration")
		case len(narrations) == 1:
			for _, n := range narrations {
				m.Narration = &n
			}
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
				m.Tracks = append(m.Tracks, RecordingEntry{Position: position, AssetID: c.h.ID, Bytes: c.h.Bytes, SourceFile: c.src.SourceFile, Title: c.rec.Title, DeclaredSeconds: c.rec.DeclaredSeconds, Readers: c.rec.Readers, Status: c.h.Status, Probe: "not_requested"})
				for _, reader := range c.rec.Readers {
					if !slices.Contains(m.Narrators, reader) {
						m.Narrators = append(m.Narrators, reader)
					}
				}
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
				h := held[t.AssetID]
				if t.Bytes > budget || (h.Format == "mp3" && t.Bytes > maxProbedTrackBytes) {
					t.Probe, t.Detail = "budget_exceeded", "the call's audio indexing budget is spent"
					ordered = false
					continue
				}
				budget -= t.Bytes
				var track audio.Track
				var probeErr error
				if h.Format == "mp3" {
					track, probeErr = probeHolding(ctx, store, h)
				} else {
					track, probeErr = probeMP4Holding(ctx, store, h)
				}
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
	byNarration := map[string]int{}
	for _, m := range r.Recordings {
		if m.Narration == nil {
			continue
		}
		i, ok := byNarration[m.Narration.ID]
		if !ok {
			i = len(r.Narrations)
			byNarration[m.Narration.ID] = i
			r.Narrations = append(r.Narrations, NarrationGroup{Narration: *m.Narration, Recordings: []RecordingRef{}})
		}
		r.Narrations[i].Recordings = append(r.Narrations[i].Recordings, RecordingRef{SourceID: m.SourceID, Format: m.Format, Kind: m.Kind, Complete: m.Complete})
	}
	sort.Slice(r.Narrations, func(i, j int) bool { return r.Narrations[i].Narration.ID < r.Narrations[j].Narration.ID })
	return r, nil
}

// probeMP4Holding reads one stored M4B or M4A at offsets, then hashes the
// whole file and rechecks its identity against the holding, and returns its
// sample-table timing and chapters in the shape of an MP3 track.
func probeMP4Holding(ctx context.Context, store *os.Root, h Holding) (audio.Track, error) {
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
	m, probeErr := audio.ProbeMP4(ctx, f, h.Bytes)
	sum := sha256.New()
	n, err := io.Copy(sum, io.LimitReader(importReader{ctx, io.NewSectionReader(f, 0, h.Bytes+1)}, h.Bytes+1))
	if err != nil {
		return audio.Track{}, err
	}
	after, statErr := store.Lstat(name)
	if n != h.Bytes || hex.EncodeToString(sum.Sum(nil)) != h.SHA256 || statErr != nil || !os.SameFile(info, after) {
		return audio.Track{}, errStoredObject
	}
	if probeErr != nil {
		return audio.Track{}, probeErr
	}
	// MP4 samples are coded frames, not per-channel PCM samples, so no
	// encoded sample count is given.
	return audio.Track{SampleRate: m.SampleRate, Channels: m.Channels, EncodedDurationMs: m.EncodedDurationMs, Chapters: m.Chapters}, nil
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
