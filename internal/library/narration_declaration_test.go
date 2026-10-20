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
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func narrationLibrary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	return root
}

func holdNarrationTracks(t *testing.T, root, item, format string, count, seed int) []ImportResult {
	t.Helper()
	out := []ImportResult{}
	for position := 1; position <= count; position++ {
		body := mp3Track(seed + position)
		packet := t.TempDir()
		receipt := audioReceipt(t, body, item, fmt.Sprintf("track-%d.mp3", position), "", "", map[string]any{"source_format": format, "position": position, "tracks": count, "title": "Same work and title"})
		writeTree(t, packet, map[string][]byte{"content.mp3": body, "receipt.json": receipt})
		r, err := Import(context.Background(), ImportRequest{Library: root, Source: packet, Apply: true})
		if err != nil {
			t.Fatal(r, err)
		}
		out = append(out, r)
	}
	return out
}

func holdNarrationSingle(t *testing.T, root, item string, seed int) ImportResult {
	t.Helper()
	body := mp4Audiobook(seed)
	packet := t.TempDir()
	writeTree(t, packet, map[string][]byte{"content.m4b": body, "receipt.json": audioReceipt(t, body, item, "Same title.m4b", "", "", nil)})
	r, err := Import(context.Background(), ImportRequest{Library: root, Source: packet, Apply: true})
	if err != nil {
		t.Fatal(r, err)
	}
	return r
}

func selectNarration(t *testing.T, root, source, format string) NarrationSelection {
	t.Helper()
	r, err := Recordings(context.Background(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range r.ByteIdentities {
		for _, ref := range identity.Recordings {
			if ref.SourceID == source && ref.Format == format {
				return NarrationSelection{SourceID: source, Format: format, Kind: ref.Kind, ByteIdentity: identity.ID}
			}
		}
	}
	t.Fatal("no complete recording", source, format, r)
	return NarrationSelection{}
}

func narrationFile(t *testing.T, d NarrationDeclaration) string {
	t.Helper()
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "declaration.json")
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func applyUserNarration(t *testing.T, root, file, retract string) NarrationResult {
	t.Helper()
	request := NarrationRequest{Library: root, File: file, Retract: retract}
	preview, err := DeclareNarration(context.Background(), request)
	if err != nil || len(preview.Plan) != 64 {
		t.Fatal(preview, err)
	}
	request.Apply, request.Plan = true, preview.Plan
	applied, err := DeclareNarration(context.Background(), request)
	if err != nil || !applied.Applied || !reflect.DeepEqual(applied.Statement, preview.Statement) {
		t.Fatal(applied, err)
	}
	return applied
}

func TestUserNarrationRelatesEncodingsAndTrackDivisionsWithoutMerging(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	first := holdNarrationTracks(t, root, "reading-a", "MP3", 2, 40)
	second := holdNarrationTracks(t, root, "reading-b", "Other MP3", 3, 60)
	mirror := holdNarrationTracks(t, root, "mirror", "MP3", 2, 40)
	single := holdNarrationSingle(t, root, "single", 30)
	otherReading := holdNarrationSingle(t, root, "another-reading", 31)
	// Curation operates on verified holdings even after all synthetic original
	// intake packets are gone; it never reads their historical source paths.
	packets := append(append(append([]ImportResult{}, first...), second...), mirror...)
	packets = append(packets, single, otherReading)
	for _, packet := range packets {
		if err := os.RemoveAll(packet.Source); err != nil {
			t.Fatal(err)
		}
	}
	selections := []NarrationSelection{selectNarration(t, root, "archive:reading-a", "MP3"), selectNarration(t, root, "archive:reading-b", "Other MP3"), selectNarration(t, root, "archive:single", "Same title.m4b"), selectNarration(t, root, "archive:mirror", "MP3")}
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:reading-one", Recordings: selections}
	file := narrationFile(t, d)
	before := importTree(t, root)
	preview, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file})
	if err != nil || preview.Statement == nil || preview.Statement.Evidence != UserNarrationEvidence || preview.Statement.Status != "matched" || preview.VerifiedAssets != 6 || preview.VerifiedBytes < 1 {
		t.Fatal(preview, err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("preview wrote library")
	}
	applied := applyUserNarration(t, root, file, "")
	after := importTree(t, root)
	delete(after, filepath.Join(".nemalo", narrationName))
	if !reflect.DeepEqual(before, after) {
		t.Fatal("a declaration changed holdings, provenance, or assets")
	}
	listed, err := Recordings(ctx, root, "")
	if err != nil || len(listed.Narrations) != 0 || len(listed.UserNarrations) != 1 || len(listed.UserNarrations[0].Recordings) != 4 || listed.UserNarrations[0].Narration.ID != d.NarrationID {
		t.Fatal(listed, err)
	}
	if len(listed.Declarations) != 1 || !listed.Declarations[0].Active || listed.Declarations[0].ID != applied.Statement.ID {
		t.Fatal(listed.Declarations)
	}
	// A mirror reuses the two exact assets and retains both source records.
	held, err := FindManaged(ctx, root, "", "all", "all", 20, 0)
	if err != nil || held.Total != 7 {
		t.Fatal(held, err)
	}
	for _, h := range held.Holdings {
		if h.ID == first[0].AssetID && len(h.Sources) != 2 {
			t.Fatal("mirror lost provenance", h)
		}
	}
	byteGroup := false
	for _, g := range listed.ByteIdentities {
		if g.ID == selections[0].ByteIdentity && len(g.Recordings) == 2 {
			byteGroup = true
		}
	}
	if !byteGroup {
		t.Fatal("user evidence replaced exact-byte evidence", listed.ByteIdentities)
	}
	// Same work/title and another reading remain distinct unless declared.
	other := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:another-reading", Recordings: []NarrationSelection{selectNarration(t, root, "archive:another-reading", "Same title.m4b")}}
	applyUserNarration(t, root, narrationFile(t, other), "")
	listed, err = Recordings(ctx, root, "")
	if err != nil || len(listed.UserNarrations) != 2 {
		t.Fatal(listed, err)
	}
	related, err := Related(ctx, RelatedRequest{Directory: root, Holding: first[0].AssetID, Limit: 20})
	if err != nil {
		t.Fatal(related, err)
	}
	found := false
	for _, g := range related.Groups {
		if g.Key == d.NarrationID {
			found = true
			if len(g.Members) != 6 || !strings.Contains(g.Members[0].Evidence[0], UserNarrationEvidence) {
				t.Fatal(g)
			}
		}
	}
	if !found {
		t.Fatal("shared related service omitted user evidence", related)
	}
	// Input ordering and a repeat declaration do not append another fact.
	before = importTree(t, root)
	slices.Reverse(d.Recordings)
	repeated := applyUserNarration(t, root, narrationFile(t, d), "")
	if !repeated.AlreadyRecorded || repeated.Statement.ID != applied.Statement.ID || !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("repeat changed recorded state", repeated)
	}
	// Retraction retains history; a new reviewed declaration can reactivate it.
	oldRetraction, err := DeclareNarration(ctx, NarrationRequest{Library: root, Retract: applied.Statement.ID})
	if err != nil {
		t.Fatal(err)
	}
	withdrawn := applyUserNarration(t, root, "", applied.Statement.ID)
	if withdrawn.Statement.Active {
		t.Fatal(withdrawn)
	}
	before = importTree(t, root)
	if !applyUserNarration(t, root, "", applied.Statement.ID).AlreadyRecorded || !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("repeat withdrawal appended")
	}
	applyUserNarration(t, root, file, "")
	before = importTree(t, root)
	if r, err := DeclareNarration(ctx, NarrationRequest{Library: root, Retract: applied.Statement.ID, Apply: true, Plan: oldRetraction.Plan}); !errors.Is(err, ErrNarrationPlanChanged) || r.Plan != "" || r.Applied {
		t.Fatal("stale withdrawal applied", r, err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("stale withdrawal wrote")
	}
}

func TestConflictingUserNarrationsAreVisibleAndRetractionResolvesThem(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	holdNarrationTracks(t, root, "first", "MP3", 2, 40)
	holdNarrationSingle(t, root, "second", 30)
	a := selectNarration(t, root, "archive:first", "MP3")
	b := selectNarration(t, root, "archive:second", "Same title.m4b")
	first := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{a, b}}
	applyUserNarration(t, root, narrationFile(t, first), "")
	conflict := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:b", Recordings: []NarrationSelection{a}}
	file := narrationFile(t, conflict)
	preview, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file})
	if err != nil || preview.Statement.Status != "unresolved" || preview.Statement.Selections[0].Status != "conflicting_user_declarations" {
		t.Fatal(preview, err)
	}
	applied := applyUserNarration(t, root, file, "")
	for _, source := range []string{"", "archive:first", "archive:second"} {
		listed, err := Recordings(ctx, root, source)
		if err != nil || len(listed.UserNarrations) != 0 {
			t.Fatal("filter hid conflict", source, listed, err)
		}
		unresolved := false
		for _, s := range listed.Declarations {
			if s.Status == "unresolved" {
				unresolved = true
			}
		}
		if !unresolved {
			t.Fatal("conflict hidden", source, listed)
		}
	}
	applyUserNarration(t, root, "", applied.Statement.ID)
	listed, err := Recordings(ctx, root, "")
	if err != nil || len(listed.UserNarrations) != 1 || listed.UserNarrations[0].Narration.ID != first.NarrationID {
		t.Fatal(listed, err)
	}
}

func TestNarrationRefusesStaleOrInsufficientSelectionsAndChangedStoredBytes(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	tracks := holdNarrationTracks(t, root, "first", "MP3", 2, 40)
	selection := selectNarration(t, root, "archive:first", "MP3")
	for _, kind := range []string{"changed_identity", "missing_selection", "provider_namespace", "bad_kind"} {
		t.Run(kind, func(t *testing.T) {
			s := selection
			id := "user-narration:reading"
			switch kind {
			case "changed_identity":
				s.ByteIdentity = "recording-sha256:" + strings.Repeat("a", 64)
			case "missing_selection":
				s.SourceID = "archive:missing"
			case "provider_namespace":
				id = "librivox:7"
			case "bad_kind":
				s.Kind = "guessed"
			}
			file := narrationFile(t, NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: id, Recordings: []NarrationSelection{s}})
			before := importTree(t, root)
			if r, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file}); err == nil || r.Plan != "" {
				t.Fatal(r, err)
			}
			if !reflect.DeepEqual(before, importTree(t, root)) {
				t.Fatal("refusal wrote")
			}
		})
	}
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:reading", Recordings: []NarrationSelection{selection}}
	file := narrationFile(t, d)
	preview, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file})
	if err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(root, filepath.FromSlash(tracks[0].Path))
	body := mustRead(t, stored)
	changed := append([]byte{}, body...)
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(stored, changed, 0600); err != nil {
		t.Fatal(err)
	}
	before := importTree(t, root)
	if r, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file, Apply: true, Plan: preview.Plan}); !errors.Is(err, errStoredObject) || r.Applied || r.Plan != "" {
		t.Fatal(r, err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("corrupt asset refusal wrote")
	}
	if err := os.WriteFile(stored, body, 0600); err != nil {
		t.Fatal(err)
	}
	applyUserNarration(t, root, file, "")
	// A new asset claiming an existing position makes the old statement visibly
	// insufficient; the declaration is never rebound to the newly held bytes.
	packet := t.TempDir()
	newBody := mp3Track(90)
	writeTree(t, packet, map[string][]byte{"content.mp3": newBody, "receipt.json": audioReceipt(t, newBody, "first", "changed.mp3", "", "", map[string]any{"source_format": "MP3", "position": 1, "tracks": 2})})
	if _, err := Import(ctx, ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
		t.Fatal(err)
	}
	listed, err := Recordings(ctx, root, "")
	if err != nil || len(listed.UserNarrations) != 0 || listed.Declarations[0].Selections[0].Status != "recording_incomplete" {
		t.Fatal(listed, err)
	}
	before = importTree(t, root)
	if _, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file}); err == nil {
		t.Fatal("incomplete declaration accepted")
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("incomplete refusal wrote")
	}
}

func TestNarrationProviderEvidenceRemainsSeparate(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	single := holdNarrationSingle(t, root, "single", 30)
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selectNarration(t, root, "archive:single", "Same title.m4b")}}
	file := narrationFile(t, d)
	applyUserNarration(t, root, file, "")
	body := mustRead(t, filepath.Join(single.Source, "content.m4b"))
	writeTree(t, single.Source, map[string][]byte{"receipt.json": audioReceipt(t, body, "single", "Same title.m4b", "7", "single", nil)})
	if _, err := Import(ctx, ImportRequest{Library: root, Source: single.Source, Apply: true}); err != nil {
		t.Fatal(err)
	}
	listed, err := Recordings(ctx, root, "")
	if err != nil || len(listed.Narrations) != 1 || listed.Narrations[0].Narration.ID != "librivox:7" || len(listed.UserNarrations) != 0 || listed.Declarations[0].Selections[0].Status != "provider_narration_evidence" {
		t.Fatal(listed, err)
	}
	before := importTree(t, root)
	if _, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file}); err == nil {
		t.Fatal("provider evidence replaced")
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("provider refusal wrote")
	}
	// A receipt cannot forge the explicit user relationship journal.
	another := holdNarrationSingle(t, root, "another", 31)
	receipt := mustRead(t, filepath.Join(another.Source, "receipt.json"))
	var raw map[string]any
	if err := json.Unmarshal(receipt, &raw); err != nil {
		t.Fatal(err)
	}
	raw["user_declaration"] = d
	receipt, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, another.Source, map[string][]byte{"receipt.json": receipt})
	beforeLog := mustRead(t, filepath.Join(root, ".nemalo", narrationName))
	if _, err := Import(ctx, ImportRequest{Library: root, Source: another.Source, Apply: true}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeLog, mustRead(t, filepath.Join(root, ".nemalo", narrationName))) {
		t.Fatal("provider created user assertion")
	}
}

func TestNarrationJournalCrashRecoveryAndStrictValidation(t *testing.T) {
	for _, room := range []int{0, 20} {
		t.Run(fmt.Sprintf("room_%d", room), func(t *testing.T) {
			ctx := context.Background()
			root := narrationLibrary(t)
			holdNarrationSingle(t, root, "single", 30)
			d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selectNarration(t, root, "archive:single", "Same title.m4b")}}
			file := narrationFile(t, d)
			preview, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file})
			if err != nil {
				t.Fatal(err)
			}
			failWrite(t, "journal", 1, room)
			if _, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file, Apply: true, Plan: preview.Plan}); !errors.Is(err, syscall.ENOSPC) {
				t.Fatal(err)
			}
			before := importTree(t, root)
			listed, err := Recordings(ctx, root, "")
			if err != nil || len(listed.Declarations) != 0 {
				t.Fatal("partial fact became evidence", listed, err)
			}
			if !reflect.DeepEqual(before, importTree(t, root)) {
				t.Fatal("read repaired journal")
			}
			writeFault = func(_ string, w io.Writer) io.Writer { return w }
			applyUserNarration(t, root, file, "")
			listed, err = Recordings(ctx, root, "")
			if err != nil || len(listed.Declarations) != 1 {
				t.Fatal(listed, err)
			}
			healthyAfter(t, root)
		})
	}
	ctx := context.Background()
	root := narrationLibrary(t)
	holdNarrationSingle(t, root, "single", 30)
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selectNarration(t, root, "archive:single", "Same title.m4b")}}
	file := narrationFile(t, d)
	applyUserNarration(t, root, file, "")
	path := filepath.Join(root, ".nemalo", narrationName)
	original := mustRead(t, path)
	for _, kind := range []string{"unknown_field", "wrong_library", "wrong_sequence", "wrong_id", "duplicate_active", "invalid_transition", "oversized_tail", "complete_garbage"} {
		t.Run(kind, func(t *testing.T) {
			var event narrationEvent
			if err := json.Unmarshal(bytes.TrimSpace(original), &event); err != nil {
				t.Fatal(err)
			}
			data := original
			switch kind {
			case "unknown_field":
				data = bytes.Replace(original, []byte(`"event"`), []byte(`"unknown"`), 1)
			case "wrong_library":
				event.LibraryID = newStateID("library")
			case "wrong_sequence":
				event.Sequence = 2
			case "wrong_id":
				event.ID = "narration-declaration-sha256:" + strings.Repeat("a", 64)
			case "duplicate_active":
				event.Sequence = 2
				b, _ := recordBytes(event)
				data = append(append([]byte{}, original...), b...)
			case "invalid_transition":
				event.Event = "retracted"
				event.Declaration = nil
			case "oversized_tail":
				data = append(append([]byte{}, original...), bytes.Repeat([]byte{'x'}, maxRecord+1)...)
			case "complete_garbage":
				data = append(append([]byte{}, original...), []byte("{}\n")...)
			}
			if kind == "wrong_library" || kind == "wrong_sequence" || kind == "wrong_id" || kind == "invalid_transition" {
				data, _ = recordBytes(event)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			before := importTree(t, root)
			if _, err := LibraryStatus(ctx, root); !errors.Is(err, ErrStateReview) {
				t.Fatal("bad relationship journal accepted", kind, err)
			}
			if _, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file}); err == nil {
				t.Fatal("bad journal previewed")
			}
			if !reflect.DeepEqual(before, importTree(t, root)) {
				t.Fatal("bad journal rewritten")
			}
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNarrationRefusesStalePlansBusyStatePendingImportsAndCancellation(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	holdNarrationSingle(t, root, "single", 30)
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selectNarration(t, root, "archive:single", "Same title.m4b")}}
	file := narrationFile(t, d)
	request := NarrationRequest{Library: root, File: file}
	preview, err := DeclareNarration(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	other := d
	other.NarrationID = "user-narration:b"
	applyUserNarration(t, root, narrationFile(t, other), "")
	request.Apply, request.Plan = true, preview.Plan
	before := importTree(t, root)
	if r, err := DeclareNarration(ctx, request); !errors.Is(err, ErrNarrationPlanChanged) || r.Applied || r.Plan != "" {
		t.Fatal(r, err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("stale plan wrote")
	}
	request.Apply, request.Plan = false, ""
	c, _, err := openControl(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DeclareNarration(ctx, request); !errors.Is(err, ErrBusy) {
		t.Fatal("writer lock bypassed", err)
	}
	if err := c.close(); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := DeclareNarration(canceled, request); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	stop := errors.New("simulated interruption")
	if _, err := importLibrary(ctx, ImportRequest{Library: root, Source: writeEPUB(t, t.TempDir()), Apply: true}, func(at string) error {
		if at == "intent" {
			return stop
		}
		return nil
	}); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	before = importTree(t, root)
	if _, err := DeclareNarration(ctx, request); err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatal("pending import ignored", err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("pending refusal wrote")
	}
}

func TestNarrationInputValidationAndLibraryScope(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	holdNarrationSingle(t, root, "single", 30)
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selectNarration(t, root, "archive:single", "Same title.m4b")}}
	body, _ := json.Marshal(d)
	for _, bad := range []string{string(body) + "{}", strings.Replace(string(body), `"schema":`, `"schema":"narration-declaration/1","schema":`, 1), strings.Replace(string(body), `"schema":`, `"unknown":`, 1), strings.Replace(string(body), "user-narration:a", "user-narration:A", 1), strings.Replace(string(body), "user-narration:a", "user-narration:-a", 1), strings.Repeat("x", maxRecord+1), string([]byte{0xff}), `null`} {
		file := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(file, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		before := importTree(t, root)
		if _, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file}); err == nil {
			t.Fatal("bad input accepted", bad)
		}
		if !reflect.DeepEqual(before, importTree(t, root)) {
			t.Fatal("bad input wrote")
		}
	}
	duplicate := d
	duplicate.Recordings = append(append([]NarrationSelection{}, d.Recordings...), d.Recordings...)
	if _, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: narrationFile(t, duplicate)}); err == nil {
		t.Fatal("duplicate recording accepted")
	}
	file := narrationFile(t, d)
	for _, request := range []NarrationRequest{{File: file}, {Library: root}, {Library: root, File: file, Retract: "x"}, {Library: root, File: file, Apply: true}, {Library: root, File: file, Plan: strings.Repeat("a", 64)}, {Library: root, Retract: "unknown"}} {
		if _, err := DeclareNarration(ctx, request); err == nil {
			t.Fatal("invalid request accepted", request)
		}
	}
	another := narrationLibrary(t)
	holdNarrationSingle(t, another, "single", 30)
	a, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file})
	if err != nil {
		t.Fatal(err)
	}
	b, err := DeclareNarration(ctx, NarrationRequest{Library: another, File: file})
	if err != nil || a.Statement.ID == b.Statement.ID || a.Plan == b.Plan {
		t.Fatal("user identity escaped library scope", a, b, err)
	}
	// Stable construction is independent of event sequence and timestamp.
	if a.Statement.ID != narrationDeclarationID(a.LibraryID, a.Statement.Declaration) {
		t.Fatal(a)
	}
}

func TestNarrationNeverInitializesAnUninitializedLibrary(t *testing.T) {
	root := t.TempDir()
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{{SourceID: "archive:test", Format: "MP3", Kind: "tracks", ByteIdentity: "recording-sha256:" + strings.Repeat("0", 64)}}}
	file := narrationFile(t, d)
	before := importTree(t, root)
	if _, err := DeclareNarration(context.Background(), NarrationRequest{Library: root, File: file, Apply: true, Plan: strings.Repeat("a", 64)}); err == nil {
		t.Fatal("uninitialized library accepted")
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("declaration created control state")
	}
}

// A complete line may survive a failure before its sync/acknowledgement.
// Retrying must recognize the same fact, flush it, and avoid a duplicate.
type completedNarrationWrite struct{ w io.Writer }

func (w completedNarrationWrite) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err != nil {
		return n, err
	}
	return n, syscall.EIO
}

func TestNarrationRecognizesACompleteUnacknowledgedAppend(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	holdNarrationSingle(t, root, "single", 30)
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selectNarration(t, root, "archive:single", "Same title.m4b")}}
	file := narrationFile(t, d)
	preview, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file})
	if err != nil {
		t.Fatal(err)
	}
	saved := writeFault
	t.Cleanup(func() { writeFault = saved })
	writeFault = func(stage string, w io.Writer) io.Writer {
		if stage == "journal" {
			return completedNarrationWrite{w}
		}
		return w
	}
	if r, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file, Apply: true, Plan: preview.Plan}); !errors.Is(err, syscall.EIO) || r.Applied {
		t.Fatal(r, err)
	}
	writeFault = saved
	before := importTree(t, root)
	retried := applyUserNarration(t, root, file, "")
	if !retried.AlreadyRecorded || !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("retry duplicated a complete fact", retried)
	}
	if got := bytes.Count(mustRead(t, filepath.Join(root, ".nemalo", narrationName)), []byte{'\n'}); got != 1 {
		t.Fatal("duplicate records", got)
	}
}

func TestNarrationBoundsAndStableDeclarationID(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	holdNarrationSingle(t, root, "single", 30)
	selection := selectNarration(t, root, "archive:single", "Same title.m4b")
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selection}}
	file := narrationFile(t, d)
	applyUserNarration(t, root, file, "")
	path := filepath.Join(root, ".nemalo", narrationName)
	original := mustRead(t, path)
	var first narrationEvent
	if err := json.Unmarshal(bytes.TrimSpace(original), &first); err != nil {
		t.Fatal(err)
	}
	// Leave a valid prefix with insufficient space for the next declaration.
	var data bytes.Buffer
	for sequence := 1; ; sequence++ {
		event := first
		event.Sequence = sequence
		if sequence%2 == 0 {
			event.Event = "retracted"
			event.Declaration = nil
		}
		b, err := recordBytes(event)
		if err != nil {
			t.Fatal(err)
		}
		if data.Len()+len(b) > maxNarrationBytes {
			break
		}
		data.Write(b)
	}
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	// A maximum-sized, still valid narration name increases this next record
	// beyond the remaining journal space without needing large audio fixtures.
	newDeclaration := d
	newDeclaration.NarrationID = "user-narration:" + strings.Repeat("a", 64)
	file = narrationFile(t, newDeclaration)
	preview, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file})
	if err != nil {
		t.Fatal(err)
	}
	before := importTree(t, root)
	if _, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file, Apply: true, Plan: preview.Plan}); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatal("journal cap not enforced", err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("journal budget refusal changed state")
	}
	// Verification counts each selected asset once and refuses >4 GiB before
	// opening files, so even repeated positions cannot defeat the byte budget.
	holdings := []Holding{}
	status := []NarrationSelectionStatus{{Assets: []string{}}}
	for i := 1; i <= 3; i++ {
		id := "sha256:" + strings.Repeat(fmt.Sprint(i), 64)
		holdings = append(holdings, Holding{ID: id, Bytes: 2 << 30})
		status[0].Assets = append(status[0].Assets, id, id)
	}
	if _, _, err := verifyNarrationAssets(ctx, root, holdings, status); err == nil || !strings.Contains(err.Error(), "4 GiB") {
		t.Fatal("verification cap bypassed", err)
	}
	tooMany := d
	tooMany.Recordings = make([]NarrationSelection, 17)
	for i := range tooMany.Recordings {
		tooMany.Recordings[i] = selection
		tooMany.Recordings[i].SourceID = fmt.Sprintf("archive:item%d", i)
	}
	if _, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: narrationFile(t, tooMany)}); err == nil {
		t.Fatal("selection cap bypassed")
	}
	vector := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{{SourceID: "archive:test", Format: "MP3", Kind: "tracks", ByteIdentity: "recording-sha256:" + strings.Repeat("0", 64)}}}
	if id := narrationDeclarationID("library:"+strings.Repeat("0", 32), vector); id != "narration-declaration-sha256:fb4d67fbbf86c0277dfb1dc29daccface4bd91d373b3d8f8d2b21529723c2991" {
		t.Fatal("declaration identity construction changed", id)
	}
}

func TestNarrationRepairsBoundedTailEvenWhenPhysicalFileExceedsJournalCap(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	holdNarrationSingle(t, root, "single", 30)
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selectNarration(t, root, "archive:single", "Same title.m4b")}}
	file := narrationFile(t, d)
	applyUserNarration(t, root, file, "")
	path := filepath.Join(root, ".nemalo", narrationName)
	var first narrationEvent
	if err := json.Unmarshal(bytes.TrimSpace(mustRead(t, path)), &first); err != nil {
		t.Fatal(err)
	}
	var prefix bytes.Buffer
	for sequence := 1; prefix.Len() < maxNarrationBytes-maxRecord/2; sequence++ {
		e := first
		e.Sequence = sequence
		if sequence%2 == 0 {
			e.Event = "retracted"
			e.Declaration = nil
		}
		b, err := recordBytes(e)
		if err != nil {
			t.Fatal(err)
		}
		prefix.Write(b)
	}
	torn := append(append([]byte{}, prefix.Bytes()...), bytes.Repeat([]byte{'x'}, maxRecord)...)
	if len(torn) <= maxNarrationBytes {
		t.Fatal("fixture not over physical cap")
	}
	if err := os.WriteFile(path, torn, 0600); err != nil {
		t.Fatal(err)
	}
	d.NarrationID = "user-narration:b"
	file = narrationFile(t, d)
	before := importTree(t, root)
	preview, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("preview trimmed tail")
	}
	r, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file, Apply: true, Plan: preview.Plan})
	if err != nil || !r.Applied {
		t.Fatal("bounded tail blocked recovery", r, err)
	}
	result := mustRead(t, path)
	if len(result) > maxNarrationBytes || !bytes.HasPrefix(result, prefix.Bytes()) || bytes.Contains(result, bytes.Repeat([]byte{'x'}, 20)) {
		t.Fatal("prefix lost or tail retained")
	}
	if _, err := LibraryStatus(ctx, root); err != nil {
		t.Fatal(err)
	}
}

func queueNarrationRescan(t *testing.T, root string, asset ImportResult) {
	t.Helper()
	c, _, err := openControl(root, true)
	if err != nil {
		t.Fatal(err)
	}
	events, lines, err := lockedManaged(c, asset.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	h := latestHoldings(lines)[asset.AssetID]
	e := operation{SchemaVersion: 1, Sequence: len(events) + 1, LibraryID: h.LibraryID, OperationID: newStateID("operation"), Event: "rescan_intent", Source: h.Path, AssetID: h.ID, Format: h.Format, Bytes: h.Bytes, Status: "checked", Reason: "checks_and_scan_recorded", At: "2026-10-20T00:00:00Z"}
	err = appendCanonical(context.Background(), c, operationsName, maxOperations, e)
	closeErr := c.close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
}

func TestNarrationWritersFinishPriorRescansAndReportStaleRefusalSeparately(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	tracks := holdNarrationTracks(t, root, "first", "MP3", 2, 40)
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selectNarration(t, root, "archive:first", "MP3")}}
	file := narrationFile(t, d)
	for i, action := range []string{"declare", "retract"} {
		request := NarrationRequest{Library: root, File: file}
		if action == "retract" {
			listed, err := Recordings(ctx, root, "")
			if err != nil {
				t.Fatal(err)
			}
			request.File = ""
			request.Retract = listed.Declarations[0].ID
		}
		preview, err := DeclareNarration(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		queueNarrationRescan(t, root, tracks[i])
		before := importTree(t, root)
		pendingPreview, err := DeclareNarration(ctx, request)
		if err != nil || pendingPreview.RecoveredRescan || pendingPreview.Plan != preview.Plan {
			t.Fatal(pendingPreview, err)
		}
		if !reflect.DeepEqual(before, importTree(t, root)) {
			t.Fatal("preview completed prior rescan")
		}
		request.Apply, request.Plan = true, preview.Plan
		applied, err := DeclareNarration(ctx, request)
		if err != nil || !applied.Applied || !applied.RecoveredRescan {
			t.Fatal(applied, err)
		}
		events, err := parseOperations(mustRead(t, filepath.Join(root, ".nemalo", operationsName)), tracks[0].LibraryID)
		if err != nil || events[len(events)-1].Event != "rescan_completed" {
			t.Fatal("prior rescan not finished", events, err)
		}
	}
	stale, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file})
	if err != nil {
		t.Fatal(err)
	}
	other := d
	other.NarrationID = "user-narration:b"
	applyUserNarration(t, root, narrationFile(t, other), "")
	third := holdNarrationSingle(t, root, "unrelated", 30)
	queueNarrationRescan(t, root, third)
	journal := mustRead(t, filepath.Join(root, ".nemalo", narrationName))
	r, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file, Apply: true, Plan: stale.Plan})
	if !errors.Is(err, ErrNarrationPlanChanged) || r.Applied || !r.RecoveredRescan || r.Plan != "" {
		t.Fatal(r, err)
	}
	if !bytes.Equal(journal, mustRead(t, filepath.Join(root, ".nemalo", narrationName))) {
		t.Fatal("stale declaration wrote a new fact")
	}
}

func TestNarrationHistoricalEvidenceAndWithdrawalWithMissingStoredBytes(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	held := holdNarrationSingle(t, root, "single", 30)
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:a", Recordings: []NarrationSelection{selectNarration(t, root, "archive:single", "Same title.m4b")}}
	file := narrationFile(t, d)
	applied := applyUserNarration(t, root, file, "")
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(held.Path))); err != nil {
		t.Fatal(err)
	}
	before := importTree(t, root)
	listed, err := Recordings(ctx, root, "archive:single")
	if err != nil || listed.Recordings[0].Tracks[0].Probe != "stored_bytes_changed" || len(listed.UserNarrations) != 1 {
		t.Fatal("current gap was hidden or historical identity changed", listed, err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("probing mutated state")
	}
	if _, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file}); !errors.Is(err, errStoredObject) {
		t.Fatal("missing bytes verified", err)
	}
	// Withdrawal concerns the statement, so missing audio cannot trap it active.
	withdrawn := applyUserNarration(t, root, "", applied.Statement.ID)
	if withdrawn.Statement.Active || withdrawn.VerifiedAssets != 0 {
		t.Fatal(withdrawn)
	}
}

func TestNarrationPreviewRefusesCanonicalRecordExpansionBeyondItsBudget(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	d := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:escaped-metadata", Recordings: []NarrationSelection{}}
	for i := 0; i < 16; i++ {
		item := fmt.Sprintf("item-%d-", i) + strings.Repeat("<", 900)
		name := strings.Repeat("<", 900) + ".m4b"
		body := mp4Audiobook(30 + i)
		packet := t.TempDir()
		writeTree(t, packet, map[string][]byte{"content.m4b": body, "receipt.json": audioReceipt(t, body, item, name, "", "", nil)})
		if _, err := Import(ctx, ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
			t.Fatal(err)
		}
		d.Recordings = append(d.Recordings, selectNarration(t, root, "archive:"+item, name))
	}
	// Raw JSON fits the input budget, while canonical stored escaping does not.
	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(d); err != nil {
		t.Fatal(err)
	}
	if input.Len() > maxRecord {
		t.Fatal("fixture does not fit input cap")
	}
	file := filepath.Join(t.TempDir(), "declaration.json")
	if err := os.WriteFile(file, input.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	before := importTree(t, root)
	if r, err := DeclareNarration(ctx, NarrationRequest{Library: root, File: file}); err == nil || r.Plan != "" || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatal("preview offered an unwritable record", r.Plan, err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("oversized preview wrote state")
	}
}
