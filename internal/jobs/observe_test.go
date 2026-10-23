package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const observedAt = "2026-01-01T00:00:00Z"

func observationLine(t *testing.T, r record) []byte {
	t.Helper()
	r.SchemaVersion, r.At = 1, observedAt
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func observationStart(t *testing.T) []byte {
	t.Helper()
	return observationLine(t, record{Event: Queued, Kind: "library_rescan", Request: json.RawMessage(`{"library":"private"}`), Context: json.RawMessage(`{"schema":"nemalo-job-library/1","library_id":"selected"}`)})
}

func observationTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			out[name] = "directory"
			return nil
		}
		data, err := os.ReadFile(name)
		out[name] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestObservePreservesAbandonedExpiredTornAndControlFiles(t *testing.T) {
	dir := t.TempDir()
	ids := []string{strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)}
	queued := observationStart(t)
	running := append(bytes.Clone(queued), observationLine(t, record{Event: Running})...)
	finished := append(bytes.Clone(running), observationLine(t, record{Event: "finished", Status: Succeeded, Result: json.RawMessage(`{"library_id":"selected"}`)})...)
	for i, data := range [][]byte{running, finished, append(bytes.Clone(running), []byte(`{"torn`)...)} {
		if err := os.WriteFile(filepath.Join(dir, ids[i]+".jsonl"), data, 0400); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{".lock", ".cancel"} {
			if err := os.WriteFile(filepath.Join(dir, ids[i]+suffix), []byte("retained"), 0400); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	before := observationTree(t, dir)
	for range 3 {
		seen := map[string]string{}
		out, err := Observe(context.Background(), dir, func(j JournalJob, integrity string) error {
			seen[j.ID] = j.Status + "/" + integrity
			return nil
		})
		if err != nil || out.Complete || !reflect.DeepEqual(out.Problems, []string{"journal_incomplete"}) || len(seen) != 3 || seen["job:"+ids[0]] != "running/complete" || seen["job:"+ids[1]] != "succeeded/complete" || seen["job:"+ids[2]] != "running/torn_tail" {
			t.Fatal(out, seen, err)
		}
	}
	if !reflect.DeepEqual(before, observationTree(t, dir)) {
		t.Fatal("passive reads mutated retained journals or control files")
	}
}

func TestObserveMissingCancelledAndCallbackFailures(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	out, err := Observe(context.Background(), missing, func(JournalJob, string) error { t.Fatal("missing callback"); return nil })
	if err != nil || !out.Complete || out.Store != "missing" {
		t.Fatal(out, err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing store was created", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, strings.Repeat("a", 32)+".jsonl"), observationStart(t), 0600); err != nil {
		t.Fatal(err)
	}
	stop := errors.New("callback stop")
	if _, err := Observe(context.Background(), dir, func(JournalJob, string) error { return stop }); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := Observe(ctx, dir, func(JournalJob, string) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := Observe(context.Background(), "relative", nil); err == nil {
		t.Fatal("invalid passive arguments accepted")
	}
}

func TestObservedJournalRejectsAmbiguousAndUnsupportedHistories(t *testing.T) {
	start := observationStart(t)
	run := observationLine(t, record{Event: Running})
	finish := observationLine(t, record{Event: "finished", Status: Succeeded})
	for name, data := range map[string][]byte{
		"unknown_schema":           bytes.Replace(start, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1),
		"unknown_field":            bytes.Replace(start, []byte(`"event":`), []byte(`"extra":true,"event":`), 1),
		"duplicate_start":          append(bytes.Clone(start), start...),
		"duplicate_nested_context": observationLine(t, record{Event: Queued, Kind: "acquire", Request: json.RawMessage(`{}`), Context: json.RawMessage(`{"library_id":"other","library_id":"selected"}`)}),
		"malformed_complete":       append(bytes.Clone(start), []byte("{bad}\n")...),
		"unknown_event":            append(bytes.Clone(start), observationLine(t, record{Event: "future"})...),
		"progress_before_running":  append(bytes.Clone(start), observationLine(t, record{Event: "progress", Progress: &Progress{Phase: "transfer"}})...),
		"negative_progress":        append(append(bytes.Clone(start), run...), observationLine(t, record{Event: "progress", Progress: &Progress{Phase: "transfer", Done: -1}})...),
		"duplicate_finish":         append(append(bytes.Clone(start), finish...), finish...),
		"blank_line":               append(bytes.Clone(start), '\n'),
		"wrong_event_shape":        append(bytes.Clone(start), observationLine(t, record{Event: Running, Kind: "acquire"})...),
		"deep_context":             observationLine(t, record{Event: Queued, Kind: "acquire", Request: json.RawMessage(`{}`), Context: json.RawMessage(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))}),
		"record_limit":             append(bytes.Clone(start), bytes.Repeat(observationLine(t, record{Event: "cancel_requested"}), MaxObservedRecords)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, _, err := parseObservedJournal(context.Background(), "job:test", data, len(data)); err == nil {
				t.Fatal("ambiguous history accepted")
			}
		})
	}
	late := append(append(bytes.Clone(start), finish...), observationLine(t, record{Event: "cancel_requested"})...)
	j, integrity, _, err := parseObservedJournal(context.Background(), "job:test", late, len(late))
	if err != nil || integrity != "complete" || j.Status != Succeeded {
		t.Fatal("late cancellation undid the terminal state", j, integrity, err)
	}
	unterminated := append(bytes.Clone(start), bytes.TrimSuffix(finish, []byte{'\n'})...)
	j, integrity, _, err = parseObservedJournal(context.Background(), "job:test", unterminated, len(unterminated))
	if err != nil || integrity != "torn_tail" || j.Status != Queued {
		t.Fatal("uncommitted terminal line was folded", j, integrity, err)
	}
}

func TestNearLimitTerminalResultIsNotSilentlyInterrupted(t *testing.T) {
	data := append(observationStart(t), observationLine(t, record{Event: Running})...)
	progress := observationLine(t, record{Event: "progress", Progress: &Progress{Phase: strings.Repeat("\x1b", 64), Unit: strings.Repeat("\x1b", 32)}})
	for len(data)+len(progress) <= maxJournal {
		data = append(data, progress...)
	}
	result := json.RawMessage(`"` + strings.Repeat("x", maxResult-2) + `"`)
	data = append(data, observationLine(t, record{Event: "finished", Status: Failed, Result: result, Error: strings.Repeat("\x1b", 2000)})...)
	if len(data) <= maxJournal+maxResult+1 || len(data) > MaxObservedJournalBytes {
		t.Fatal("fixture does not cross the old reader boundary", len(data))
	}
	dir := t.TempDir()
	id := "job:" + strings.Repeat("a", 32)
	if err := os.WriteFile(filepath.Join(dir, fileID(id)+".jsonl"), data, 0600); err != nil {
		t.Fatal(err)
	}
	s := &Store{dir: dir}
	j, err := s.Get(id) // A falsely unfinished read would append interruption.
	if err != nil || j.Status != Failed || j.Result != strings.Repeat("x", maxResult-2) {
		t.Fatal("terminal result lost", j.Status, err)
	}
	if after, err := os.ReadFile(filepath.Join(dir, fileID(id)+".jsonl")); err != nil || !bytes.Equal(data, after) {
		t.Fatal("valid terminal journal was reconciled", err)
	}
	out, err := Observe(context.Background(), dir, func(j JournalJob, integrity string) error {
		if j.Status != Failed || len(j.Result) != maxResult || integrity != "complete" {
			t.Fatal("passive terminal result lost", j.Status, integrity)
		}
		return nil
	})
	if err != nil || !out.Complete || out.ReadBytes != len(data) {
		t.Fatal(out, err)
	}
}

func TestObserveBoundsActualReadsAndDirectoryWork(t *testing.T) {
	dir := t.TempDir()
	data := observationStart(t)
	data = append(data, []byte(strings.Repeat("x", MaxObservedJournalBytes-len(data)))...)
	for i := 0; i < 7; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%032x.jsonl", i)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Observe(context.Background(), dir, func(JournalJob, string) error { return nil })
	if err != nil || out.Complete || out.ReadBytes != MaxObservedReadBytes || !strings.Contains(strings.Join(out.Problems, ","), "journal_read_limit") {
		t.Fatal(out, err)
	}
	dir = t.TempDir()
	for i := 0; i <= MaxObservedEntries; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprint(i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	out, err = Observe(context.Background(), dir, func(JournalJob, string) error { return nil })
	if err != nil || out.Complete || out.Entries != MaxObservedEntries || !reflect.DeepEqual(out.Problems, []string{"directory_limit"}) {
		t.Fatal(out, err)
	}
	// An exact-ceiling directory cannot be called complete by spending an
	// additional uncounted EOF probe. Both cases consume the same allowance.
	if err := os.Remove(filepath.Join(dir, fmt.Sprint(MaxObservedEntries))); err != nil {
		t.Fatal(err)
	}
	out, err = Observe(context.Background(), dir, func(JournalJob, string) error { return nil })
	if err != nil || out.Complete || out.Entries != MaxObservedEntries || !reflect.DeepEqual(out.Problems, []string{"directory_limit"}) {
		t.Fatal(out, err)
	}
}

func TestJobContextIsOptionalBoundedAndImmutable(t *testing.T) {
	s, _ := open(t, 1, 0)
	binding := map[string]string{"schema": "test/1", "library_id": "selected"}
	j, err := s.StartWithContext("demo", nil, binding, func(context.Context, func(Progress)) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	binding["library_id"] = "changed"
	j = waitFinal(t, s, j.ID)
	if asJSON(j.Context) != `{"library_id":"selected","schema":"test/1"}` {
		t.Fatal("context changed after start", j.Context)
	}
	if _, err := s.StartWithContext("demo", nil, strings.Repeat("x", maxContext), nil); err == nil {
		t.Fatal("oversized context accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := Observe(ctx, s.dir, func(j JournalJob, integrity string) error {
		if string(j.Context) != `{"library_id":"selected","schema":"test/1"}` || integrity != "complete" {
			t.Fatal(j, integrity)
		}
		return nil
	})
	if err != nil || !out.Complete {
		t.Fatal(out, err)
	}
}

func TestObserveRefusesFinalLinksReplacementsAndOversizedEntries(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(outside, observationStart(t), 0600); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, strings.Repeat("a", 32)+".jsonl")
	if err := os.Symlink(outside, name); err != nil {
		t.Fatal("native file-link fixture unavailable", err)
	}
	called := false
	out, err := Observe(context.Background(), dir, func(JournalJob, string) error { called = true; return nil })
	if err != nil || out.Complete || out.ReadBytes != 0 || called {
		t.Fatal("final linked journal was read", out, err)
	}
	linkedDir := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(dir, linkedDir); err != nil {
		t.Fatal("native directory-link fixture unavailable", err)
	}
	if _, err := Observe(context.Background(), linkedDir, func(JournalJob, string) error { t.Fatal("linked directory callback"); return nil }); err == nil {
		t.Fatal("final linked directory accepted")
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, bytes.Repeat([]byte{'x'}, MaxObservedJournalBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = Observe(context.Background(), dir, func(JournalJob, string) error { t.Fatal("oversized callback"); return nil })
	if err != nil || out.Complete || out.ReadBytes != 0 {
		t.Fatal("oversized entry was read", out, err)
	}
	if _, err := (&Store{dir: dir}).read("job:" + strings.Repeat("a", 32)); err == nil {
		t.Fatal("legacy overrun was silently folded")
	}
	if err := os.WriteFile(name, observationStart(t), 0600); err != nil {
		t.Fatal(err)
	}
	var replacementErr error
	_, err = Observe(context.Background(), dir, func(JournalJob, string) error {
		replacementErr = os.Rename(dir, dir+"-original")
		if replacementErr != nil {
			return replacementErr
		}
		return os.Mkdir(dir, 0700)
	})
	t.Cleanup(func() {
		if replacementErr == nil {
			_ = os.RemoveAll(dir)
			_ = os.Rename(dir+"-original", dir)
		}
	})
	if err == nil {
		t.Fatal("directory replacement did not invalidate visited records")
	}
}

func TestObservationReadHonorsCancellationBeforeReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, err := (observationReader{ctx, strings.NewReader("private")}).Read(make([]byte, 4)); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal(n, err)
	}
}
