package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/safeio"
)

func open(t *testing.T, running, pending int) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir, running, pending)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); _ = s.Wait(context.Background()) })
	return s, dir
}

func waitFinal(t *testing.T, s *Store, id string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if final(j.Status) {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}

func TestJobsRecordResultsFailuresAndPanics(t *testing.T) {
	s, _ := open(t, 2, 4)
	ok, err := s.Start("demo", map[string]string{"a": "b"}, func(ctx context.Context, report func(Progress)) (any, error) {
		report(Progress{Phase: "transfer", Done: 1, Total: 2, Unit: "bytes"})
		return map[string]int{"n": 3}, nil
	})
	if err != nil || ok.Status != Queued && ok.Status != Running && ok.Status != Succeeded || ok.Kind != "demo" || !strings.HasPrefix(ok.ID, "job:") {
		t.Fatal(ok, err)
	}
	j := waitFinal(t, s, ok.ID)
	if j.Status != Succeeded || asJSON(j.Result) != `{"n":3}` || asJSON(j.Request) != `{"a":"b"}` || j.Progress == nil || j.Progress.Done != 1 {
		t.Fatalf("%+v", j)
	}
	bad, _ := s.Start("demo", nil, func(context.Context, func(Progress)) (any, error) {
		return map[string]string{"partial": "yes"}, errors.New("source refused")
	})
	if j := waitFinal(t, s, bad.ID); j.Status != Failed || j.Error != "source refused" || asJSON(j.Result) != `{"partial":"yes"}` {
		t.Fatalf("%+v", j)
	}
	boom, _ := s.Start("demo", nil, func(context.Context, func(Progress)) (any, error) { panic("bug") })
	if j := waitFinal(t, s, boom.ID); j.Status != Failed || !strings.Contains(j.Error, "unexpectedly") {
		t.Fatalf("%+v", j)
	}
	list, err := s.List()
	if err != nil || len(list) != 3 {
		t.Fatal(len(list), err)
	}
	if _, err := s.Get("job:nope"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

// Jobs beyond the running slots wait; beyond the queue they are refused.
// Cancelling a queued or running job records cancelled.
func TestJobsQueueCancelAndRefuseWhenFull(t *testing.T) {
	s, _ := open(t, 1, 1)
	release := make(chan struct{})
	// Release the fixture before open's Wait cleanup, even if an assertion fails.
	t.Cleanup(func() { close(release) })
	block := func(ctx context.Context, _ func(Progress)) (any, error) {
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	started := make(chan struct{})
	first, err := s.Start("demo", nil, func(ctx context.Context, report func(Progress)) (any, error) {
		close(started)
		return block(ctx, report)
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, started)
	second, err := s.Start("demo", nil, block)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start("demo", nil, block); !errors.Is(err, ErrBusy) {
		t.Fatal("a third job was accepted", err)
	}
	if j, _ := s.Get(second.ID); j.Status != Queued || j.Owner != "this_process" {
		t.Fatalf("second %+v", j)
	}
	if _, err := s.Cancel(second.ID); err != nil {
		t.Fatal(err)
	}
	if j := waitFinal(t, s, second.ID); j.Status != Cancelled {
		t.Fatalf("queued cancel %+v", j)
	}
	if _, err := s.Cancel(first.ID); err != nil {
		t.Fatal(err)
	}
	if j := waitFinal(t, s, first.ID); j.Status != Cancelled {
		t.Fatalf("running cancel %+v", j)
	}
	if _, err := s.Cancel(first.ID); err == nil {
		t.Fatal("a finished job was cancelled again")
	}
	s.Close()
	if _, err := s.Start("demo", nil, block); err == nil {
		t.Fatal("a closing store started a job")
	}
}

// A slot is occupied only once the callback starts, independent of goroutine
// scheduling. Starting two jobs does not promise which callback runs first.
func waitStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("job callback did not start")
	}
}

// A job whose process ended is recorded as interrupted by the next reader; one
// whose lock another process holds is reported as running there.
func TestJobsOfAnEndedProcessAreInterrupted(t *testing.T) {
	dir := t.TempDir()
	id := "job:" + strings.Repeat("ab", 16)
	line, _ := json.Marshal(record{SchemaVersion: 1, Event: Queued, At: time.Now().UTC().Format(time.RFC3339Nano), Kind: "acquire", Request: json.RawMessage(`{}`)})
	running, _ := json.Marshal(record{SchemaVersion: 1, Event: Running, At: time.Now().UTC().Format(time.RFC3339Nano)})
	if err := os.WriteFile(filepath.Join(dir, fileID(id)+".jsonl"), append(append(append(line, '\n'), running...), []byte("\n{\"torn")...), 0o600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, fileID(id)+".lock")
	holder, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := safeio.TryLock(holder, true); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if j, err := s.Get(id); err != nil || j.Status != Running || j.Owner != "another_process" {
		t.Fatalf("held %+v %v", j, err)
	}
	if j, err := s.Cancel(id); err != nil || !strings.Contains(j.Detail, "cancel requested") {
		t.Fatal(j, err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileID(id)+".cancel")); err != nil {
		t.Fatal("no cancel marker", err)
	}
	_ = safeio.Unlock(holder)
	_ = holder.Close()
	j, err := s.Get(id)
	if err != nil || j.Status != Interrupted || !strings.Contains(j.Detail, "ended") {
		t.Fatalf("released %+v %v", j, err)
	}
	// A second reader sees the same final state, recorded once.
	if again, _ := s.Get(id); again.Status != Interrupted || again.Updated != j.Updated {
		t.Fatalf("again %+v", again)
	}
}

func TestOpenRemovesOldFinishedJobsAndRejectsBadFolders(t *testing.T) {
	if _, err := Open("relative", 1, 0); err == nil {
		t.Fatal("relative folder accepted")
	}
	dir := t.TempDir()
	id := "job:" + strings.Repeat("cd", 16)
	old := time.Now().Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
	var lines []byte
	for _, r := range []record{{SchemaVersion: 1, Event: Queued, At: old, Kind: "x", Request: json.RawMessage(`{}`)}, {SchemaVersion: 1, Event: "finished", At: old, Status: Succeeded}} {
		b, _ := json.Marshal(r)
		lines = append(append(lines, b...), '\n')
	}
	path := filepath.Join(dir, fileID(id)+".jsonl")
	if err := os.WriteFile(path, lines, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an old finished job was kept")
	}
}

func asJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Another process cancels a job through a marker the owner polls for, and
// jobs queued when the server closes do not start.
func TestJobsCancelAcrossProcessesAndDoNotStartAfterClose(t *testing.T) {
	dir := t.TempDir()
	owner, err := Open(dir, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	t.Cleanup(func() {
		close(release)
		owner.Close()
		_ = owner.Wait(context.Background())
	})
	other, err := Open(dir, 1, 4)
	if err != nil {
		t.Fatal(err)
	}
	block := func(ctx context.Context, _ func(Progress)) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return nil, nil
		}
	}
	started := make(chan struct{})
	running, err := owner.Start("demo", nil, func(ctx context.Context, report func(Progress)) (any, error) {
		close(started)
		return block(ctx, report)
	})
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, started)
	queued, err := owner.Start("demo", nil, block)
	if err != nil {
		t.Fatal(err)
	}
	owner.Close()
	if j, err := other.Cancel(running.ID); err != nil || j.Owner != "another_process" {
		t.Fatal(j, err)
	}
	if j := waitFinal(t, owner, running.ID); j.Status != Cancelled {
		t.Fatalf("%+v", j)
	}
	if j := waitFinal(t, owner, queued.ID); j.Status != Cancelled || !strings.Contains(j.Detail, "closed") {
		t.Fatalf("%+v", j)
	}
	_ = owner.Wait(context.Background())
}

// Work that returned success is recorded as succeeded even if a cancel came
// at the end; a failure after a cancel keeps its own error.
func TestALateCancelDoesNotHideSuccess(t *testing.T) {
	s, _ := open(t, 1, 0)
	started, finish := make(chan struct{}), make(chan struct{})
	j, _ := s.Start("demo", nil, func(ctx context.Context, _ func(Progress)) (any, error) {
		close(started)
		<-finish
		return map[string]bool{"complete": true}, nil
	})
	<-started
	if _, err := s.Cancel(j.ID); err != nil {
		t.Fatal(err)
	}
	close(finish)
	if got := waitFinal(t, s, j.ID); got.Status != Succeeded {
		t.Fatalf("%+v", got)
	}
}
