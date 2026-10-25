package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyTree copies a committed fixture into a fresh directory, so tests never
// write inside testdata.
func copyTree(t *testing.T, from string) string {
	t.Helper()
	to := filepath.Join(t.TempDir(), "library")
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return to
}

// testdata/alpha2-library was written by the published 0.1.0-alpha.2 release:
// library init, then one unscanned EPUB import (decision 0096). Every later
// build must open it in place, without migration, and keep using it.
const alpha2LibraryID = "library:ed7fdabdf4d15c0d12e706ec565cfdc7"

func TestAlpha2LibraryOpensInPlace(t *testing.T) {
	ctx := context.Background()
	lib := copyTree(t, filepath.Join("testdata", "alpha2-library"))
	status, err := LibraryStatus(ctx, lib)
	// The identity alpha.2 generated is kept, not reissued.
	if err != nil || status.Status != "initialized" || status.LibraryID != alpha2LibraryID {
		t.Fatal(status, err)
	}
	check, err := CheckLibrary(ctx, lib)
	if err != nil || !check.Healthy || check.Holdings != 1 || check.Verified != 1 || check.Review["unscanned"] != 1 {
		t.Fatalf("alpha.2 holding did not verify: %+v %v", check, err)
	}
	if _, err := Import(ctx, ImportRequest{Library: lib, Source: writeEPUBParagraph(t, t.TempDir(), "Added by a later build"), Apply: true}); err != nil {
		t.Fatal("import into an alpha.2 library:", err)
	}
	if _, err := EnableBookmarks(ctx, lib); err != nil {
		t.Fatal("bookmarks in an alpha.2 library:", err)
	}
	check, err = CheckLibrary(ctx, lib)
	if err != nil || !check.Healthy || check.Holdings != 2 || check.Verified != 2 {
		t.Fatalf("after later writes: %+v %v", check, err)
	}
}

func isNewer(err error) bool {
	return errors.Is(err, ErrStateReview) && strings.Contains(err.Error(), "may come from a newer Nemalo")
}

// A record from a newer schema is refused exactly as before, now with a
// reason. A malformed record of the current version keeps the plain refusal.
func TestNewerRecordsExplainTheirRefusal(t *testing.T) {
	const id = "library:0123456789abcdef0123456789abcdef"
	newer := []byte(`{"schema_version":2,"library_id":"` + id + `","added":"field"}` + "\n")
	malformed := []byte(`{"schema_version":1,"library_id":"` + id + `","added":"field"}` + "\n")
	for name, parse := range map[string]func([]byte) error{
		journalName:    func(b []byte) error { _, err := parseInitializationJournal(b); return err },
		operationsName: func(b []byte) error { _, err := parseOperations(b, id); return err },
		holdingsName:   func(b []byte) error { _, err := parseHoldings(b, id); return err },
		bookmarkName:   func(b []byte) error { _, err := parseBookmarkJournal(b, id); return err },
		narrationName:  func(b []byte) error { _, err := parseNarrationJournal(b, id); return err },
	} {
		if err := parse(newer); !isNewer(err) || !strings.Contains(err.Error(), name+" has schema_version 2") {
			t.Errorf("%s: newer record gave %v", name, err)
		}
		if err := parse(malformed); !errors.Is(err, ErrStateReview) || isNewer(err) {
			t.Errorf("%s: malformed current record gave %v", name, err)
		}
	}
}

func TestNewerControlFilesAndCleanupJournalExplainTheirRefusal(t *testing.T) {
	ctx := context.Background()
	lib := t.TempDir()
	state, err := Initialize(ctx, lib)
	if err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(lib, ".nemalo")
	if err := os.WriteFile(filepath.Join(control, "future.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LibraryStatus(ctx, lib); !isNewer(err) || !strings.Contains(err.Error(), `unknown control file "future.jsonl"`) {
		t.Fatal("unknown control file:", err)
	}
	if err := os.Remove(filepath.Join(control, "future.jsonl")); err != nil {
		t.Fatal(err)
	}
	// A directory is not a control file of any version.
	if err := os.Mkdir(filepath.Join(control, "future"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LibraryStatus(ctx, lib); !errors.Is(err, ErrStateReview) || isNewer(err) {
		t.Fatal("unexpected directory:", err)
	}
	if err := os.Remove(filepath.Join(control, "future")); err != nil {
		t.Fatal(err)
	}
	line := `{"schema_version":2,"sequence":1,"library_id":"` + state.LibraryID + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(control, cleanupName), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckLibrary(ctx, lib); !isNewer(err) || !strings.Contains(err.Error(), cleanupName) {
		t.Fatal("newer cleanup record:", err)
	}
}

func TestIntakeReceiptVersionGatesImport(t *testing.T) {
	for version, newer := range map[string]bool{`"schema_version":2,`: true, ``: false, `"schema_version":0,`: false} {
		dir := t.TempDir()
		receipt := `{` + version + `"complete":true,"status":"untrusted_intake","transfer":{"bytes":1,"sha256":"` + strings.Repeat("a", 64) + `"}}`
		if err := os.WriteFile(filepath.Join(dir, "receipt.json"), []byte(receipt), 0o600); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		_, err = readReceipt(root)
		root.Close()
		if err == nil || strings.Contains(err.Error(), "newer version") != newer {
			t.Errorf("receipt %q: %v", version, err)
		}
	}
}

func TestCleanupManifestVersionIsChecked(t *testing.T) {
	for version, ok := range map[int]bool{1: true, 2: false} {
		store, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		const lib = "library:0123456789abcdef0123456789abcdef"
		id := "cleanup:" + strings.Repeat("b", 32)
		m := cleanupManifest{SchemaVersion: version, CleanupID: id, LibraryID: lib, Folder: "/folder", Digest: strings.Repeat("c", 64), Entries: []CleanupEntry{}}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.FromSlash(holdingSlash(id))
		if err := store.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := store.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		started := cleanupRecord{CleanupID: id, Manifest: hex.EncodeToString(sum[:]), Folder: m.Folder, Digest: m.Digest, Count: 0}
		_, err = readManifest(store, started, lib)
		store.Close()
		if (err == nil) != ok {
			t.Errorf("manifest version %d: %v", version, err)
		}
	}
}
