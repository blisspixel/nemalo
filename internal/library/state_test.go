package library

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLibraryInitializationStatusAndReaderWriterLocks(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "my-book.epub")
	if err := os.WriteFile(content, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LibraryStatus(context.Background(), root)
	if err != nil || s.Status != "not_initialized" {
		t.Fatal(s, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("status wrote control state")
	}
	s, err = Initialize(context.Background(), root)
	if err != nil || !s.Created || s.Resumed || s.Status != "initialized" || s.JournalRecords != 2 || !validStateID(s.LibraryID, "library") || s.InitializedAt == nil || s.InitializedAt.IsZero() {
		t.Fatal(s, err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".nemalo", journalName))
	if err != nil {
		t.Fatal(err)
	}
	again, err := Initialize(context.Background(), root)
	if err != nil || again.Created || again.Resumed || again.LibraryID != s.LibraryID {
		t.Fatal(again, err)
	}
	after, err := os.ReadFile(filepath.Join(root, ".nemalo", journalName))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("repeat appended or changed journal", err)
	}
	s, err = LibraryStatus(context.Background(), root)
	if err != nil || s.Status != "initialized" {
		t.Fatal(s, err)
	}
	c, _, err := openControl(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LibraryStatus(context.Background(), root); err != nil {
		t.Fatal("shared readers conflict", err)
	}
	if _, err := Initialize(context.Background(), root); !errors.Is(err, ErrBusy) {
		t.Fatal("writer ignored reader", err)
	}
	if err := c.close(); err != nil {
		t.Fatal(err)
	}
	c, _, err = openControl(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LibraryStatus(context.Background(), root); !errors.Is(err, ErrBusy) {
		t.Fatal("reader ignored writer", err)
	}
	if _, err := Initialize(context.Background(), root); !errors.Is(err, ErrBusy) {
		t.Fatal("second writer entered", err)
	}
	if err := c.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal("lock did not release", err)
	}
	book, err := os.ReadFile(content)
	if err != nil || string(book) != "original" {
		t.Fatal("content changed", err)
	}
}

func TestInitializationInterruptionAndCancellation(t *testing.T) {
	root := t.TempDir()
	stop := errors.New("simulated interruption after synced intent")
	s, err := initialize(context.Background(), root, func(stage string) error {
		if stage != "started" {
			t.Fatal(stage)
		}
		return stop
	})
	if !errors.Is(err, stop) || s.Status != "initialization_pending" || s.JournalRecords != 1 {
		t.Fatal(s, err)
	}
	pending, err := LibraryStatus(context.Background(), root)
	if !errors.Is(err, ErrInitializationPending) || pending.LibraryID != s.LibraryID || pending.JournalRecords != 1 {
		t.Fatal(pending, err)
	}
	resumed, err := Initialize(context.Background(), root)
	if err != nil || !resumed.Resumed || resumed.Created || resumed.LibraryID != s.LibraryID || resumed.Status != "initialized" {
		t.Fatal(resumed, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	untouched := t.TempDir()
	if _, err := Initialize(ctx, untouched); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := LibraryStatus(ctx, untouched); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(untouched, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled operation wrote")
	}
	root = t.TempDir()
	ctx, cancel = context.WithCancel(context.Background())
	_, err = initialize(ctx, root, func(string) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if p, err := LibraryStatus(context.Background(), root); !errors.Is(err, ErrInitializationPending) || p.JournalRecords != 1 {
		t.Fatal(p, err)
	}
}

func TestJournalRejectsTornUnknownAndInconsistentState(t *testing.T) {
	root := t.TempDir()
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, ".nemalo", journalName)
	good, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(good[:len(good)-1], []byte{'\n'})
	var first, second journalEvent
	if err = json.Unmarshal(lines[0], &first); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(lines[1], &second); err != nil {
		t.Fatal(err)
	}
	encode := func(a, b journalEvent) []byte {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		return append(append(append(x, '\n'), y...), '\n')
	}
	cases := map[string][]byte{
		"empty": {}, "torn": good[:len(good)-4], "missing-newline": good[:len(good)-1],
		"too-large": bytes.Repeat([]byte{'x'}, maxJournalBytes+1), "blank-line": append(append([]byte{}, good...), '\n'),
		"unknown-field":   bytes.Replace(good, []byte(`"sequence":1`), []byte(`"unknown":1,"sequence":1`), 1),
		"duplicate-field": bytes.Replace(good, []byte(`"sequence":1`), []byte(`"sequence":9,"sequence":1`), 1),
		"whitespace":      append([]byte{' '}, good...), "bad-json": []byte("{\n"),
	}
	change := second
	change.LibraryID = newStateID("library")
	cases["identity-changed"] = encode(first, change)
	change = second
	change.OperationID = newStateID("operation")
	cases["operation-changed"] = encode(first, change)
	change = second
	change.Sequence = 3
	cases["sequence-gap"] = encode(first, change)
	change = second
	change.Event = "initialization_started"
	cases["transition"] = encode(first, change)
	change = first
	change.SchemaVersion = 2
	cases["future-schema"] = encode(change, second)
	change = first
	change.Event = "import_started"
	cases["unknown-operation"] = encode(change, second)
	change = first
	change.LibraryID = "library:BAD"
	cases["bad-id"] = encode(change, second)
	change = first
	change.At = time.Time{}
	cases["zero-time"] = encode(change, second)
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(file, bad, 0600); err != nil {
				t.Fatal(err)
			}
			if s, err := LibraryStatus(context.Background(), root); !errors.Is(err, ErrStateReview) || s.Status != "needs_review" {
				t.Fatal(s, err)
			}
			if _, err := Initialize(context.Background(), root); !errors.Is(err, ErrStateReview) {
				t.Fatal("corruption accepted", err)
			}
			preserved, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(preserved, bad) {
				t.Fatal("corruption truncated or repaired", err)
			}
		})
	}
}

func TestStateBoundariesAndUnknownArtifacts(t *testing.T) {
	for _, name := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		if _, err := Initialize(context.Background(), name); err == nil {
			t.Fatal("implicit/missing root created", name)
		}
		if _, err := LibraryStatus(context.Background(), name); err == nil {
			t.Fatal("implicit/missing root accepted", name)
		}
	}
	for _, kind := range []string{"file", "empty-control", "lock-only", "unknown-extra", "journal-link", "lock-link", "control-link", "nonempty-lock"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, ".nemalo")
			switch kind {
			case "file":
				if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "empty-control", "lock-only":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "lock-only" {
					if err := os.WriteFile(filepath.Join(path, lockName), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "control-link":
				target := t.TempDir()
				if err := os.Symlink(target, path); err != nil {
					t.Skip("symlinks unavailable", err)
				}
			default:
				if _, err := Initialize(context.Background(), root); err != nil {
					t.Fatal(err)
				}
				if kind == "unknown-extra" {
					if err := os.WriteFile(filepath.Join(path, "unexpected"), []byte("keep"), 0600); err != nil {
						t.Fatal(err)
					}
				} else if kind == "nonempty-lock" {
					if err := os.WriteFile(filepath.Join(path, lockName), []byte("unrecognized owner"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					name := journalName
					if kind == "lock-link" {
						name = lockName
					}
					if err := os.Link(filepath.Join(path, name), filepath.Join(t.TempDir(), "outside")); err != nil {
						t.Skip("hard links unavailable", err)
					}
				}
			}
			if _, err := Initialize(context.Background(), root); !errors.Is(err, ErrStateReview) {
				t.Fatal("untrusted state accepted", kind, err)
			}
			if _, err := LibraryStatus(context.Background(), root); !errors.Is(err, ErrStateReview) {
				t.Fatal("untrusted status accepted", kind, err)
			}
		})
	}
}

func TestProcessCrashReleasesLockAndResumesIntent(t *testing.T) {
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStateLockHelper$")
	cmd.Env = append(os.Environ(), "NEMALO_LOCK_HELPER_ROOT="+root)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	line, err := bufio.NewReader(pipe).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatal(line, err)
	}
	if _, err := Initialize(context.Background(), root); !errors.Is(err, ErrBusy) {
		t.Fatal("another process entered writer", err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err == nil {
		t.Fatal("helper was not killed")
	}
	s, err := Initialize(context.Background(), root)
	if err != nil || !s.Resumed || s.Status != "initialized" {
		t.Fatal("process lock remained stale", s, err)
	}
}

func TestStateLockHelper(t *testing.T) {
	root := os.Getenv("NEMALO_LOCK_HELPER_ROOT")
	if root == "" {
		return
	}
	_, err := initialize(context.Background(), root, func(string) error {
		fmt.Println("locked")
		time.Sleep(time.Minute)
		return errors.New("helper timeout")
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStateDescriptorReplacementAndFailures(t *testing.T) {
	root := t.TempDir()
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	c, _, err := openControl(root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	f, err := openRegular(c.root, journalName, os.O_RDWR|os.O_APPEND)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readJournal(f); err == nil {
		t.Fatal("closed journal read succeeded")
	}
	if err := appendEvent(context.Background(), c, f, journalEvent{}); err == nil {
		t.Fatal("closed journal appended")
	}
	if err := os.Rename(filepath.Join(root, ".nemalo", lockName), filepath.Join(root, ".nemalo", "original-lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".nemalo", lockName), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := sameEntry(c.root, lockName, c.lock); !errors.Is(err, ErrStateReview) {
		t.Fatal("replaced lock accepted", err)
	}
	if validStateID(strings.Repeat("x", 40), "library") || validStateID("", "library") {
		t.Fatal("invalid state identity")
	}
}
