package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/scanner"
)

type metadataNoScan struct{ t *testing.T }

func (s metadataNoScan) Scan(context.Context, string) scanner.Result {
	s.t.Fatal("metadata fixture invoked a scanner")
	return scanner.Result{}
}

func metadataBook(t *testing.T) (string, ImportResult) {
	t.Helper()
	root := t.TempDir()
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUBMetadata(t, t.TempDir(), "Source text.", `<dc:identifier id="isbn" opf:scheme="ISBN">9780306406157</dc:identifier>`)
	r, err := Import(context.Background(), ImportRequest{Library: root, Source: source, Apply: true, Scanner: metadataNoScan{t}})
	if err != nil {
		t.Fatal(err)
	}
	return root, r
}

func TestHoldingMetadataUsesSharedLookupWithoutOpeningOrChangingBodies(t *testing.T) {
	ctx := context.Background()
	root, imported := metadataBook(t)
	// An empty optional statement journal is a valid complete prefix.
	if err := os.WriteFile(filepath.Join(root, ".nemalo", narrationName), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// Historical metadata stays readable even when the stored body is absent.
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(imported.Path))); err != nil {
		t.Fatal(err)
	}
	before := importTree(t, root)
	for range 3 {
		err := WithHoldingMetadata(ctx, root, imported.AssetID, func(view HoldingMetadata) error {
			if view.ByteStatus != "not_revalidated" || view.AssetID != imported.AssetID || len(view.Identities) != 1 || view.Identities[0].ID != "isbn:9780306406157" || view.Titles[0] != "Test" || len(view.HoldingSHA256) != 64 {
				t.Fatal(view)
			}
			data, err := json.Marshal(view)
			if err != nil || strings.Contains(string(data), root) || strings.Contains(string(data), `"path"`) || strings.Contains(string(data), "declared_url") {
				t.Fatal("private paths/URLs escaped", string(data), err)
			}
			// Content resolves through a second shared handle while this view's
			// lock excludes every writer, including in the same process.
			asset, id, err := ManagedAsset(ctx, root, imported.AssetID)
			if err != nil || asset.ID != view.AssetID || id != view.LibraryID {
				t.Fatal(asset, id, err)
			}
			if _, err := Initialize(ctx, root); !errors.Is(err, ErrBusy) {
				t.Fatal("metadata reader allowed writer", err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("metadata changed library files")
	}
}

func TestHoldingMetadataRefusesMissingCanceledCorruptAndOversizedState(t *testing.T) {
	root, imported := metadataBook(t)
	for _, name := range []string{"missing", "invalid_asset", "cancelled", "callback_error", "nil_callback", "torn", "oversized"} {
		t.Run(name, func(t *testing.T) {
			libraryRoot, asset := root, imported.AssetID
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "missing" {
				libraryRoot = filepath.Join(t.TempDir(), "missing")
			}
			if name == "invalid_asset" {
				asset = "sha256:bad"
			}
			if name == "cancelled" {
				cancel()
			}
			if name == "torn" || name == "oversized" {
				freshRoot, freshImport := metadataBook(t)
				libraryRoot, asset = freshRoot, freshImport.AssetID
				if name == "torn" {
					f, err := os.OpenFile(filepath.Join(libraryRoot, ".nemalo", holdingsName), os.O_APPEND|os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := f.WriteString("{"); err != nil {
						t.Fatal(err)
					}
					if err := f.Close(); err != nil {
						t.Fatal(err)
					}
					if _, _, err := ManagedAsset(ctx, libraryRoot, asset); err != nil {
						t.Fatal("ordinary content lost its committed-prefix contract", err)
					}
				} else {
					file := filepath.Join(libraryRoot, ".nemalo", holdingsName)
					data, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					var holding Holding
					if err := json.Unmarshal(data, &holding); err != nil {
						t.Fatal(err)
					}
					source := holding.Sources[0]
					source.DeclaredRights = make([]string, 32)
					for i := range source.DeclaredRights {
						source.DeclaredRights[i] = strings.Repeat("\u202e", 600)
					}
					holding.Sources = []Source{source, source, source}
					encoded, err := json.Marshal(holding)
					if err != nil {
						t.Fatal(err)
					}
					if len(encoded) >= MaxHoldingMetadataBytes {
						t.Fatal("fixture must exceed the safe wire cap through escaping, not raw JSON size")
					}
					if err := os.WriteFile(file, append(encoded, '\n'), 0600); err != nil {
						t.Fatal(err)
					}
					hash := sha256.Sum256(encoded)
					rewriteImportOperations(t, libraryRoot, func(e *operation) { e.ImportHoldingSHA256 = hex.EncodeToString(hash[:]) })
					if _, _, err := ManagedAsset(ctx, libraryRoot, asset); err != nil {
						t.Fatal("oversized fixture is not valid canonical state", err)
					}
				}
			}
			var before map[string]string
			if name != "missing" {
				before = importTree(t, libraryRoot)
			}
			calls := 0
			sentinel := errors.New("reader stopped")
			callback := func(HoldingMetadata) error { calls++; return sentinel }
			if name == "nil_callback" {
				callback = nil
			}
			err := WithHoldingMetadata(ctx, libraryRoot, asset, callback)
			if err == nil {
				t.Fatal("failed metadata read succeeded")
			}
			if name == "callback_error" {
				if calls != 1 || !errors.Is(err, sentinel) {
					t.Fatal(calls, err)
				}
			} else if calls != 0 {
				t.Fatal("invalid metadata reached callback", calls, err)
			}
			if name == "oversized" && !errors.Is(err, ErrMetadataBudget) {
				t.Fatal(err)
			}
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if name == "missing" {
				if _, err := os.Stat(libraryRoot); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing directory was created", err)
				}
			} else if !reflect.DeepEqual(before, importTree(t, libraryRoot)) {
				t.Fatal("failed metadata read mutated state")
			}
		})
	}
}

func TestHoldingMetadataReusesCanonicalRecordingIdentityWithoutProbing(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"original", "mirror"} {
		for position := 1; position <= 2; position++ {
			ingestIdentityTrack(t, root, source, "MP3", position, 2, 40+position)
		}
	}
	ingestIdentityTrack(t, root, "incomplete", "MP3", 1, 2, 41)
	canonical, err := Recordings(ctx, root, "")
	if err != nil {
		t.Fatal(err)
	}
	asset := canonical.Recordings[0].Tracks[0].AssetID
	held, _, err := ManagedAsset(ctx, root, asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(held.Locations[0].Path))); err != nil {
		t.Fatal(err)
	}
	before := importTree(t, root)
	err = WithHoldingMetadata(ctx, root, asset, func(view HoldingMetadata) error {
		if len(view.Recordings) != 3 || view.ByteStatus != "not_revalidated" {
			t.Fatal(view)
		}
		for _, r := range view.Recordings {
			if !reflect.DeepEqual(r.Positions, []int{1}) {
				t.Fatal(r)
			}
			if r.Selection.SourceID == "archive:incomplete" {
				if r.ByteIdentity != "" || r.Selection.Complete || !reflect.DeepEqual(r.Missing, []int{2}) {
					t.Fatal("incomplete recording borrowed identity", r)
				}
			} else if r.ByteIdentity != canonical.ByteIdentities[0].ID || r.ByteIdentitySchema != RecordingByteIdentityVersion {
				t.Fatal("canonical byte identity changed", r)
			}
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("recording metadata changed files", err)
	}
}

func TestHoldingMetadataRetainsUserAssertionsWithdrawalAndConflictingTracks(t *testing.T) {
	ctx := context.Background()
	root := narrationLibrary(t)
	tracks := holdNarrationTracks(t, root, "first", "MP3", 2, 40)
	selection := selectNarration(t, root, "archive:first", "MP3")
	declaration := NarrationDeclaration{Schema: NarrationDeclarationSchema, NarrationID: "user-narration:metadata", Recordings: []NarrationSelection{selection}}
	applied := applyUserNarration(t, root, narrationFile(t, declaration), "")
	read := func() HoldingMetadata {
		t.Helper()
		var out HoldingMetadata
		if err := WithHoldingMetadata(ctx, root, tracks[0].AssetID, func(view HoldingMetadata) error { out = view; return nil }); err != nil {
			t.Fatal(err)
		}
		return out
	}
	active := read()
	if len(active.Recordings) != 1 || len(active.Recordings[0].UserNarrations) != 1 || len(active.UserDeclarations) != 1 || !active.UserDeclarations[0].Active || active.UserDeclarations[0].Evidence != UserNarrationEvidence {
		t.Fatal(active)
	}
	applyUserNarration(t, root, "", applied.Statement.ID)
	withdrawn := read()
	if len(withdrawn.Recordings[0].UserNarrations) != 0 || len(withdrawn.UserDeclarations) != 1 || withdrawn.UserDeclarations[0].Active || withdrawn.UserDeclarations[0].Status != "retracted" || withdrawn.HoldingSHA256 != active.HoldingSHA256 {
		t.Fatal(withdrawn)
	}
	// A second physical asset in the same declared position must keep the
	// selected original asset's relation and expose the canonical conflict.
	ingestIdentityTrack(t, root, "first", "MP3", 1, 2, 55)
	conflict := read()
	if len(conflict.Recordings) != 1 || conflict.Recordings[0].Selection.Complete || len(conflict.Recordings[0].Findings) == 0 || conflict.Recordings[0].ByteIdentity != "" || len(conflict.UserDeclarations) != 1 || conflict.HoldingSHA256 != active.HoldingSHA256 {
		t.Fatal(conflict)
	}
	if !reflect.DeepEqual(conflict.Recordings[0].Positions, []int{1}) {
		t.Fatal("conflicting position dropped selected asset", conflict)
	}
	for _, result := range []RecordingsResult{{ByteIdentities: []RecordingByteIdentity{{}}}, {UserNarrations: []NarrationGroup{{}}}, {Recordings: []RecordingManifest{{}}}, {Declarations: []NarrationDeclarationStatus{{}}}} {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if err := projectMetadataRecordings(cancelled, &HoldingMetadata{}, result); !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled metadata projection proceeded", err)
		}
	}
}
