package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func scopeLibrary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".nemalo")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var data []byte
	for i, event := range []string{"initialization_started", "initialization_completed"} {
		b, err := json.Marshal(journalEvent{SchemaVersion: 1, Sequence: i + 1, LibraryID: "library:" + strings.Repeat("a", 32), OperationID: "operation:" + strings.Repeat("b", 32), Event: event, At: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		data = append(append(data, b...), '\n')
	}
	for name, b := range map[string][]byte{journalName: data, lockName: nil} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestJobScopeReadsCanonicalHoldingsWithoutBodiesOrWrites(t *testing.T) {
	root, held := metadataBook(t)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(held.Path))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".nemalo", narrationName), nil, 0400); err != nil {
		t.Fatal(err)
	}
	before := importTree(t, root)
	for range 3 {
		err := WithJobScope(context.Background(), root, func(scope JobScope) error {
			if scope.Catalog != "observed" || len(scope.Holdings) != 1 || scope.Holdings[0].ID != held.AssetID || scope.LibraryID != scope.Holdings[0].LibraryID {
				t.Fatal(scope)
			}
			if _, err := Initialize(context.Background(), root); !errors.Is(err, ErrBusy) {
				t.Fatal("callback did not retain shared lock", err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("scope read changed the library")
	}
}

func TestJobScopeObservesOnlyImmutableIdentityDuringWriter(t *testing.T) {
	root := scopeLibrary(t)
	// Invalid mutable bytes prove the busy branch does not parse that journal.
	if err := os.WriteFile(filepath.Join(root, ".nemalo", operationsName), []byte("uncommitted writer work"), 0600); err != nil {
		t.Fatal(err)
	}
	before := importTree(t, root)
	// Windows byte-range locks reject reads through a second handle even
	// for an empty file. Inventory the lock bytes outside the lock interval;
	// every busy-state assertion still runs under the actual exclusive lock.
	err := func() (err error) {
		c, _, err := openControl(root, true)
		if err != nil {
			return err
		}
		defer func() { err = errors.Join(err, c.close()) }()
		if _, err := LibraryStatus(context.Background(), root); !errors.Is(err, ErrBusy) {
			t.Fatal("ordinary status lock was weakened", err)
		}
		return WithJobScope(context.Background(), root, func(scope JobScope) error {
			if scope.Catalog != "busy" || scope.LibraryID != "library:"+strings.Repeat("a", 32) || scope.Holdings != nil {
				t.Fatal("writer conferred mutable catalog evidence", scope)
			}
			return nil
		})
	}()
	if err != nil || !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("busy identity observation failed or wrote", err)
	}
}

func TestJobScopeRejectsMissingPendingCorruptTornAndUnboundedControl(t *testing.T) {
	for _, name := range []string{"missing", "uninitialized", "pending", "torn_init", "unknown_init", "torn_operations", "bad_holdings", "extra_control", "huge_control", "cancelled", "nil_callback"} {
		t.Run(name, func(t *testing.T) {
			root := scopeLibrary(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "missing":
				root = filepath.Join(t.TempDir(), "missing")
			case "uninitialized":
				root = t.TempDir()
			case "pending", "torn_init", "unknown_init":
				p := filepath.Join(root, ".nemalo", journalName)
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				if name == "pending" {
					b = b[:bytes.IndexByte(b, '\n')+1]
				} else if name == "torn_init" {
					b = b[:len(b)-1]
				} else {
					b = bytes.ReplaceAll(b, []byte(`"schema_version":1`), []byte(`"schema_version":2`))
				}
				if err := os.WriteFile(p, b, 0600); err != nil {
					t.Fatal(err)
				}
			case "torn_operations", "bad_holdings":
				n := operationsName
				if name == "bad_holdings" {
					n = holdingsName
				}
				if err := os.WriteFile(filepath.Join(root, ".nemalo", n), []byte("{bad"), 0600); err != nil {
					t.Fatal(err)
				}
			case "extra_control", "huge_control":
				count := 1
				if name == "huge_control" {
					count = 100
				}
				for i := 0; i < count; i++ {
					if err := os.WriteFile(filepath.Join(root, ".nemalo", fmt.Sprint("unexpected", i)), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "cancelled":
				cancel()
			}
			var use func(JobScope) error
			if name != "nil_callback" {
				use = func(JobScope) error { t.Fatal("invalid identity reached callback"); return nil }
			}
			if err := WithJobScope(ctx, root, use); err == nil {
				t.Fatal("invalid library accepted")
			}
			if name == "missing" {
				if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing library created", err)
				}
			}
			if name == "uninitialized" {
				if _, err := os.Stat(filepath.Join(root, ".nemalo")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("uninitialized library changed", err)
				}
			}
		})
	}
}

func TestJobScopeInvalidatesCallbackOnCancellationAndIdentityChange(t *testing.T) {
	for _, name := range []string{"callback_failure", "cancelled", "rewrite_init", "replace_init", "replace_control", "replace_root"} {
		t.Run(name, func(t *testing.T) {
			root := scopeLibrary(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stop := errors.New("callback failed")
			err := WithJobScope(ctx, root, func(JobScope) error {
				if name == "callback_failure" {
					return stop
				}
				if name == "cancelled" {
					cancel()
					return nil
				}
				file := filepath.Join(root, ".nemalo", journalName)
				if name == "rewrite_init" {
					b, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					return os.WriteFile(file, bytes.ReplaceAll(b, []byte(strings.Repeat("a", 32)), []byte(strings.Repeat("c", 32))), 0600)
				}
				if name == "replace_control" {
					file = filepath.Join(root, ".nemalo")
				} else if name == "replace_root" {
					file = root
				}
				if err := os.Rename(file, file+"-original"); err != nil {
					// Some platforms refuse replacing open capabilities. That
					// refusal must itself invalidate the operation, not be skipped.
					return err
				}
				t.Cleanup(func() {
					_ = os.RemoveAll(file)
					_ = os.Rename(file+"-original", file)
				})
				if name == "replace_init" {
					return os.WriteFile(file, []byte("replacement"), 0600)
				}
				return os.Mkdir(file, 0700)
			})
			if err == nil || name == "callback_failure" && !errors.Is(err, stop) || name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("invalid callback result accepted", err)
			}
		})
	}
}
