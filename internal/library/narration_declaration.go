package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// NarrationDeclarationSchema fixes user statement fields and their meaning.
const NarrationDeclarationSchema = "narration-declaration/1"

// UserNarrationEvidence identifies an explicit local assertion, never a receipt.
const UserNarrationEvidence = "user_declaration/1"
const narrationName = "narrations.jsonl"
const maxNarrationBytes = 1 << 20
const maxNarrationVerificationBytes int64 = 4 << 30

// ErrNarrationPlanChanged refuses an unreviewed relationship change. Prior
// rescan recovery, if completed, is reported separately by the result.
var ErrNarrationPlanChanged = errors.New("the narration preview no longer matches current library or declaration evidence; no new relationship fact was written; preview again")

// NarrationSelection pins a recording selection to its complete ordered bytes.
// The digest uses recording-bytes/1, not titles or acoustic matching.
type NarrationSelection struct {
	SourceID     string `json:"source_id"`
	Format       string `json:"source_format"`
	Kind         string `json:"kind"`
	ByteIdentity string `json:"byte_identity"`
}

// NarrationDeclaration is a user's assertion, scoped by the library identity.
// It is never populated from a provider receipt.
type NarrationDeclaration struct {
	Schema      string               `json:"schema"`
	NarrationID string               `json:"narration_id"`
	Recordings  []NarrationSelection `json:"recordings"`
}

type NarrationSelectionStatus struct {
	Selection NarrationSelection `json:"selection"`
	Status    string             `json:"status"`
	// Assets are included in write previews, not duplicated in every historical
	// statement returned by a recording listing.
	Assets []string `json:"ordered_assets,omitempty"`
}

// NarrationDeclarationStatus retains unresolved and withdrawn statements so
// missing, conflicting, or changed selections cannot silently acquire identity.
type NarrationDeclarationStatus struct {
	ID          string                     `json:"id"`
	Evidence    string                     `json:"evidence"`
	Declaration NarrationDeclaration       `json:"declaration"`
	Active      bool                       `json:"active"`
	Status      string                     `json:"status"`
	Selections  []NarrationSelectionStatus `json:"selections"`
}

type NarrationRequest struct {
	Library string
	File    string
	Retract string
	Apply   bool
	Plan    string
}

// NarrationResult previews or records a single relationship fact. It never
// changes holdings, audio, provenance, scan disposition, or source files.
type NarrationResult struct {
	SchemaVersion   int                         `json:"schema_version"`
	Library         string                      `json:"library"`
	LibraryID       string                      `json:"library_id,omitempty"`
	Action          string                      `json:"action"`
	ApplyRequested  bool                        `json:"apply_requested"`
	Applied         bool                        `json:"applied"`
	AlreadyRecorded bool                        `json:"already_recorded"`
	RecoveredRescan bool                        `json:"recovered_rescan,omitempty"`
	Plan            string                      `json:"plan,omitempty"`
	Statement       *NarrationDeclarationStatus `json:"statement,omitempty"`
	VerifiedAssets  int                         `json:"verified_assets"`
	VerifiedBytes   int64                       `json:"verified_bytes"`
	Limitations     []string                    `json:"limitations"`
}

type narrationEvent struct {
	SchemaVersion int                   `json:"schema_version"`
	Sequence      int                   `json:"sequence"`
	LibraryID     string                `json:"library_id"`
	Event         string                `json:"event"`
	ID            string                `json:"id"`
	Declaration   *NarrationDeclaration `json:"declaration,omitempty"`
	At            string                `json:"at"`
}

type narrationView struct {
	events       []narrationEvent
	declarations map[string]NarrationDeclaration
	active       map[string]bool
	digest       string
}

func validUserNarrationID(id string) bool {
	name, ok := strings.CutPrefix(id, "user-narration:")
	if !ok || len(name) < 1 || len(name) > 64 {
		return false
	}
	for i, c := range name {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			continue
		}
		if i == 0 || c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func narrationSelectionKey(s NarrationSelection) string {
	return s.SourceID + "\x00" + s.Format + "\x00" + s.Kind
}

func normalizeNarrationDeclaration(d *NarrationDeclaration) error {
	if d.Schema != NarrationDeclarationSchema || !validUserNarrationID(d.NarrationID) || len(d.Recordings) < 1 || len(d.Recordings) > 16 {
		return errors.New("a narration declaration needs schema narration-declaration/1, a user-narration:NAME ID, and 1 to 16 recording selections")
	}
	for _, s := range d.Recordings {
		hex, ok := strings.CutPrefix(s.ByteIdentity, "recording-sha256:")
		if !validNote(s.SourceID, 1000) || !validNote(s.Format, 1000) || !utf8.ValidString(s.SourceID) || !utf8.ValidString(s.Format) || (s.Kind != "tracks" && s.Kind != "single_file") || !ok || !validHash(hex) {
			return errors.New("each narration selection needs an exact source, format, kind, and recording-sha256: byte identity")
		}
	}
	sort.Slice(d.Recordings, func(i, j int) bool {
		return narrationSelectionKey(d.Recordings[i]) < narrationSelectionKey(d.Recordings[j])
	})
	for i := 1; i < len(d.Recordings); i++ {
		if narrationSelectionKey(d.Recordings[i-1]) == narrationSelectionKey(d.Recordings[i]) {
			return errors.New("a recording selection appears twice")
		}
	}
	return nil
}

func narrationDeclarationID(libraryID string, d NarrationDeclaration) string {
	digest, _ := canonicalDigest(struct {
		Library     string               `json:"library_id"`
		Declaration NarrationDeclaration `json:"declaration"`
	}{libraryID, d})
	return "narration-declaration-sha256:" + digest
}

func readNarrationDeclaration(ctx context.Context, name string) (NarrationDeclaration, error) {
	var d NarrationDeclaration
	absolute, err := filepath.Abs(name)
	if err != nil {
		return d, err
	}
	root, err := safeio.OpenResolvedRoot(filepath.Dir(absolute))
	if err != nil {
		return d, err
	}
	defer root.Close()
	base := filepath.Base(absolute)
	info, err := root.Lstat(base)
	if err != nil {
		return d, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxRecord {
		return d, errors.New("a narration declaration must be a regular file of at most 64 KiB")
	}
	f, err := safeio.OpenRegular(root, base, info, os.O_RDONLY)
	if err != nil {
		return d, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(importReader{ctx, f}, maxRecord+1))
	if err != nil {
		return d, err
	}
	if len(data) > maxRecord || int64(len(data)) != info.Size() || !utf8.Valid(data) {
		return d, errors.New("narration declaration size or UTF-8 is invalid or changed while reading")
	}
	if err := uniqueKeys(data); err != nil {
		return d, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, err
	}
	if len(bytes.TrimSpace(data[dec.InputOffset():])) != 0 {
		return d, errors.New("data follows the narration declaration")
	}
	return d, normalizeNarrationDeclaration(&d)
}

// readNarrationJournal accepts no fact from an incomplete final line. The
// complete prefix must remain canonical, bounded, and valid under replay.
func readNarrationJournal(root *os.Root, libraryID string) (narrationView, error) {
	var empty narrationView
	info, err := root.Lstat(narrationName)
	if errors.Is(err, os.ErrNotExist) {
		return parseNarrationJournal(nil, libraryID)
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxNarrationBytes+maxRecord {
		return empty, ErrStateReview
	}
	f, err := openRegular(root, narrationName, os.O_RDONLY)
	if err != nil {
		return empty, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, info.Size()+1))
	if err != nil || int64(len(data)) != info.Size() {
		return empty, ErrStateReview
	}
	return parseNarrationJournal(data, libraryID)
}

func parseNarrationJournal(data []byte, libraryID string) (narrationView, error) {
	v := narrationView{declarations: map[string]NarrationDeclaration{}, active: map[string]bool{}}
	if len(data) > maxNarrationBytes+maxRecord {
		return v, ErrStateReview
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		cut := bytes.LastIndexByte(data, '\n') + 1
		if len(data)-cut > maxRecord {
			return v, ErrStateReview
		}
		data = data[:cut]
	}
	if len(data) > maxNarrationBytes {
		return v, ErrStateReview
	}
	if len(data) > 0 {
		for _, line := range bytes.Split(data[:len(data)-1], []byte{'\n'}) {
			if err := newerRecord(line, narrationName); err != nil {
				return v, err
			}
			var e narrationEvent
			if len(line)+1 > maxRecord || canonical(line, &e) != nil || e.SchemaVersion != 1 || e.Sequence != len(v.events)+1 || e.LibraryID != libraryID {
				return v, ErrStateReview
			}
			at, err := time.Parse(time.RFC3339Nano, e.At)
			if err != nil || at.UTC().Format(time.RFC3339Nano) != e.At {
				return v, ErrStateReview
			}
			switch e.Event {
			case "declared":
				if e.Declaration == nil || normalizeNarrationDeclaration(e.Declaration) != nil || e.ID != narrationDeclarationID(libraryID, *e.Declaration) || v.active[e.ID] {
					return v, ErrStateReview
				}
				// Normalization may not change canonical on-disk selection order.
				encoded, _ := json.Marshal(e)
				if !bytes.Equal(encoded, line) {
					return v, ErrStateReview
				}
				v.declarations[e.ID] = *e.Declaration
				v.active[e.ID] = true
			case "retracted":
				if e.Declaration != nil || !v.active[e.ID] {
					return v, ErrStateReview
				}
				v.active[e.ID] = false
			default:
				return v, ErrStateReview
			}
			v.events = append(v.events, e)
		}
	}
	v.digest, _ = canonicalDigest(v.events)
	return v, nil
}

// DeclareNarration applies only a reviewed local relationship statement. A
// complete, synced append is the entire operation; there is no asset mutation.
func DeclareNarration(ctx context.Context, request NarrationRequest) (result NarrationResult, err error) {
	result = NarrationResult{SchemaVersion: 1, Library: request.Library, ApplyRequested: request.Apply, Action: "declare", Limitations: []string{
		"This is a user's assertion over exact held recording selections, not provider evidence or proof of the same reading. The declaration changes only relationship statements; prior rescan recovery is separate.",
		"Journal-only retrieval is historical evidence; source probing and library check report current stored-byte gaps separately.",
		"A relationship write is one synced journal fact. Directory-entry power-loss durability depends on the filesystem.",
	}}
	if request.Library == "" || (request.File == "") == (request.Retract == "") || request.Plan != "" && !request.Apply {
		return result, errors.New("narration needs an explicit library and exactly one declaration file or retraction ID; a plan applies only with apply")
	}
	if request.Apply && !validHash(request.Plan) {
		return result, errors.New("narration apply requires the plan digest from its preview")
	}
	if request.Retract != "" {
		result.Action = "retract"
		digest, ok := strings.CutPrefix(request.Retract, "narration-declaration-sha256:")
		if !ok || !validHash(digest) {
			return result, errors.New("retract requires an exact narration-declaration-sha256: ID")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var d NarrationDeclaration
	if request.File != "" {
		d, err = readNarrationDeclaration(ctx, request.File)
		if err != nil {
			return result, err
		}
	}
	root, err := filepath.Abs(request.Library)
	if err != nil {
		return result, err
	}
	result.Library = root
	state, err := LibraryStatus(ctx, root)
	if err != nil {
		return result, err
	}
	if state.Status != "initialized" {
		return result, errors.New("narration declarations require an initialized library")
	}
	c, _, err := openControl(root, request.Apply)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, c.close()) }()
	init, err := readInit(c)
	if err != nil {
		return result, err
	}
	if len(init) != 2 || init[0].LibraryID != state.LibraryID {
		return result, errors.New("narration declarations require an initialized library")
	}
	result.LibraryID = init[0].LibraryID
	events, lines, err := lockedManaged(c, result.LibraryID)
	if err != nil {
		return result, err
	}
	if request.Apply {
		result.RecoveredRescan = len(events) > 0 && events[len(events)-1].Event == "rescan_intent"
		events, lines, err = rollForwardRescan(ctx, c, events, lines)
		if err != nil {
			result.RecoveredRescan = false
			return result, err
		}
	}
	if len(events) > 0 && events[len(events)-1].Event == "import_intent" {
		return result, pendingError(events[len(events)-1].Source)
	}
	view, err := readNarrationJournal(c.root, result.LibraryID)
	if err != nil {
		return result, err
	}
	id := request.Retract
	if id != "" {
		var ok bool
		d, ok = view.declarations[id]
		if !ok {
			return result, errors.New("no recorded narration declaration has that ID")
		}
	} else {
		id = narrationDeclarationID(result.LibraryID, d)
	}
	base := RecordingsResult{SchemaVersion: 1, Library: root, LibraryID: result.LibraryID, Recordings: []RecordingManifest{}, Narrations: []NarrationGroup{}}
	base, err = recordingManifests(ctx, base, lines, "")
	if err != nil {
		return result, err
	}
	statuses := narrationSelectionStatuses(d, recordingSelectionIndex(base.Recordings), true)
	if result.Action == "declare" {
		for _, s := range statuses {
			if s.Status != "matched" {
				return result, fmt.Errorf("narration selection %s %s: %s", s.Selection.SourceID, s.Selection.Format, s.Status)
			}
		}
		result.VerifiedAssets, result.VerifiedBytes, err = verifyNarrationAssets(ctx, root, lines, statuses)
		if err != nil {
			return result, err
		}
	}
	result.AlreadyRecorded = view.active[id] == (result.Action == "declare")
	event := narrationEvent{SchemaVersion: 1, Sequence: len(view.events) + 1, LibraryID: result.LibraryID, Event: "declared", ID: id, At: time.Now().UTC().Format(time.RFC3339Nano)}
	var eventLine []byte
	if !result.AlreadyRecorded {
		if result.Action == "declare" {
			event.Declaration = &d
		} else {
			event.Event = "retracted"
		}
		eventLine, err = recordBytes(event)
		if err != nil {
			return result, err
		}
	}
	// Preview the resulting relationship state without changing any journal.
	proposed := narrationView{events: view.events, declarations: map[string]NarrationDeclaration{}, active: map[string]bool{}}
	for key, value := range view.declarations {
		proposed.declarations[key] = value
		proposed.active[key] = view.active[key]
	}
	proposed.declarations[id] = d
	proposed.active[id] = result.Action == "declare"
	if err = decorateUserNarrations(ctx, &base, proposed); err != nil {
		return result, err
	}
	for i := range base.Declarations {
		if base.Declarations[i].ID == id {
			result.Statement = &base.Declarations[i]
			for j := range result.Statement.Selections {
				result.Statement.Selections[j].Assets = statuses[j].Assets
			}
			break
		}
	}
	digest, _ := canonicalDigest(struct {
		Schema  string `json:"schema"`
		Library string `json:"library_id"`
		Action  string `json:"action"`
		ID      string `json:"declaration_id"`
		Journal string `json:"journal_sha256"`
	}{"narration-plan/1", result.LibraryID, result.Action, id, view.digest})
	if request.Apply && request.Plan != digest {
		return result, ErrNarrationPlanChanged
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	result.Plan = digest
	if !request.Apply {
		return result, nil
	}
	if !result.AlreadyRecorded {
		prefixBytes := 0
		for _, e := range view.events {
			b, err := recordBytes(e)
			if err != nil {
				return result, err
			}
			prefixBytes += len(b)
		}
		if prefixBytes+len(eventLine) > maxNarrationBytes {
			return result, errors.New("narration journal exceeds its byte limit; existing records were preserved")
		}
		if err := repairNarrationTail(ctx, c, result.LibraryID, view.digest); err != nil {
			if errors.Is(err, ErrNarrationPlanChanged) {
				result.Plan = ""
			}
			return result, err
		}
		if err := appendCanonical(ctx, c, narrationName, maxNarrationBytes, event); err != nil {
			return result, err
		}
	} else {
		// A previous call may have appended a complete line but failed before
		// its sync or acknowledgement. A successful retry must flush it too.
		f, err := openRegular(c.root, narrationName, os.O_RDWR)
		if err != nil {
			return result, err
		}
		syncErr := f.Sync()
		if err = errors.Join(syncErr, f.Close()); err != nil {
			return result, err
		}
	}
	result.Applied = true
	return result, nil
}

// Repair only the incomplete last line after checking the complete prefix
// through the same opened file. Make room before the shared append's cap check.
func repairNarrationTail(ctx context.Context, c *control, libraryID, digest string) error {
	info, err := c.root.Lstat(narrationName)
	if errors.Is(err, os.ErrNotExist) {
		empty, _ := parseNarrationJournal(nil, libraryID)
		if empty.digest != digest {
			return ErrNarrationPlanChanged
		}
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxNarrationBytes+maxRecord {
		return ErrStateReview
	}
	f, err := openRegular(c.root, narrationName, os.O_RDWR)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(importReader{ctx, f}, info.Size()+1))
	if err != nil {
		return err
	}
	if int64(len(data)) != info.Size() {
		return ErrStateReview
	}
	view, err := parseNarrationJournal(data, libraryID)
	if err != nil {
		return err
	}
	if view.digest != digest {
		return ErrNarrationPlanChanged
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		_, err = dropTornTail(f, int64(len(data)))
		return err
	}
	return nil
}

func verifyNarrationAssets(ctx context.Context, root string, lines []Holding, statuses []NarrationSelectionStatus) (int, int64, error) {
	held := latestHoldings(lines)
	ids := []string{}
	seen := map[string]bool{}
	var size int64
	for _, s := range statuses {
		for _, id := range s.Assets {
			if seen[id] {
				continue
			}
			seen[id] = true
			h, ok := held[id]
			if !ok || h.Bytes > maxNarrationVerificationBytes-size {
				return 0, 0, errors.New("narration byte verification exceeds its 4 GiB budget")
			}
			size += h.Bytes
			ids = append(ids, id)
		}
	}
	store, err := safeio.OpenRoot(root)
	if err != nil {
		return 0, 0, err
	}
	defer store.Close()
	for _, id := range ids {
		h := held[id]
		obj, err := inspectStored(ctx, store, h.Path, h.Bytes, h.SHA256)
		if err != nil {
			return 0, 0, err
		}
		if !obj.present || !obj.exclusive {
			return 0, 0, errStoredObject
		}
	}
	return len(ids), size, nil
}

func recordingSelectionIndex(manifests []RecordingManifest) map[string]RecordingManifest {
	index := make(map[string]RecordingManifest, len(manifests))
	for _, m := range manifests {
		index[narrationSelectionKey(NarrationSelection{SourceID: m.SourceID, Format: m.Format, Kind: m.Kind})] = m
	}
	return index
}

func narrationSelectionStatuses(d NarrationDeclaration, manifests map[string]RecordingManifest, includeAssets bool) []NarrationSelectionStatus {
	result := []NarrationSelectionStatus{}
	for _, selection := range d.Recordings {
		s := NarrationSelectionStatus{Selection: selection, Status: "recording_not_held", Assets: []string{}}
		if m, ok := manifests[narrationSelectionKey(selection)]; ok {
			if includeAssets {
				for _, track := range m.Tracks {
					s.Assets = append(s.Assets, track.AssetID)
				}
			}
			switch {
			case !m.Complete:
				s.Status = "recording_incomplete"
			case recordingByteIdentities([]RecordingManifest{m})[0].ID != selection.ByteIdentity:
				s.Status = "ordered_assets_changed"
			case m.Narration != nil || slices.Contains(m.Findings, "held tracks name different narrations") || slices.Contains(m.Findings, "some held tracks name no narration"):
				s.Status = "provider_narration_evidence"
			default:
				s.Status = "matched"
			}
		}
		result = append(result, s)
	}
	return result
}

func decorateUserNarrations(ctx context.Context, result *RecordingsResult, view narrationView) error {
	result.Declarations = []NarrationDeclarationStatus{}
	result.UserNarrations = []NarrationGroup{}
	statements := []NarrationDeclarationStatus{}
	narrations := []NarrationGroup{}
	ids := make([]string, 0, len(view.declarations))
	for id := range view.declarations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	claims := map[string]map[string]bool{}
	manifests := recordingSelectionIndex(result.Recordings)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return err
		}
		d := view.declarations[id]
		s := NarrationDeclarationStatus{ID: id, Evidence: UserNarrationEvidence, Declaration: d, Active: view.active[id], Status: "retracted", Selections: narrationSelectionStatuses(d, manifests, false)}
		if s.Active {
			s.Status = "matched"
			for _, selection := range s.Selections {
				if selection.Status != "matched" {
					s.Status = "unresolved"
					continue
				}
				key := narrationSelectionKey(selection.Selection)
				if claims[key] == nil {
					claims[key] = map[string]bool{}
				}
				claims[key][d.NarrationID] = true
			}
		}
		statements = append(statements, s)
	}
	groups := map[string]map[string]RecordingRef{}
	for i := range statements {
		if err := ctx.Err(); err != nil {
			return err
		}
		s := &statements[i]
		if !s.Active {
			continue
		}
		for j := range s.Selections {
			selection := &s.Selections[j]
			if selection.Status == "matched" && len(claims[narrationSelectionKey(selection.Selection)]) > 1 {
				selection.Status = "conflicting_user_declarations"
				s.Status = "unresolved"
			}
		}
		if s.Status != "matched" {
			continue
		}
		id := s.Declaration.NarrationID
		if groups[id] == nil {
			groups[id] = map[string]RecordingRef{}
		}
		for _, selection := range s.Selections {
			ref := selection.Selection
			groups[id][narrationSelectionKey(ref)] = RecordingRef{SourceID: ref.SourceID, Format: ref.Format, Kind: ref.Kind, Complete: true}
		}
	}
	names := make([]string, 0, len(groups))
	for id := range groups {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, name := range names {
		group := NarrationGroup{Narration: Narration{ID: name, Evidence: UserNarrationEvidence}, Recordings: []RecordingRef{}}
		keys := make([]string, 0, len(groups[name]))
		for key := range groups[name] {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			group.Recordings = append(group.Recordings, groups[name][key])
		}
		narrations = append(narrations, group)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result.Declarations, result.UserNarrations = statements, narrations
	return nil
}

func filterRecordingResult(r *RecordingsResult, source string) {
	if source == "" {
		return
	}
	r.Recordings = slices.DeleteFunc(r.Recordings, func(m RecordingManifest) bool { return m.SourceID != source })
	filter := func(groups []NarrationGroup) []NarrationGroup {
		for i := range groups {
			groups[i].Recordings = slices.DeleteFunc(groups[i].Recordings, func(ref RecordingRef) bool { return ref.SourceID != source })
		}
		return slices.DeleteFunc(groups, func(g NarrationGroup) bool { return len(g.Recordings) == 0 })
	}
	r.Narrations = filter(r.Narrations)
	r.UserNarrations = filter(r.UserNarrations)
	r.ByteIdentities = recordingByteIdentities(r.Recordings)
	r.Declarations = slices.DeleteFunc(r.Declarations, func(s NarrationDeclarationStatus) bool {
		for _, selection := range s.Declaration.Recordings {
			if selection.SourceID == source {
				return false
			}
		}
		return true
	})
}
