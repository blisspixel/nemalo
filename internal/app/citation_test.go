package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type citationNoScan struct{ t *testing.T }

func (s citationNoScan) Scan(context.Context, string) scanner.Result {
	s.t.Fatal("citation invoked a scanner")
	return scanner.Result{}
}

func citationTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			out[rel] = "directory"
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCitationExportsRecordedMetadataWithoutOpeningBodiesOrWritingState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "book.epub")
	if err := os.WriteFile(source, epubWithText("Synthetic source."), 0600); err != nil {
		t.Fatal(err)
	}
	service := Service{Scanner: citationNoScan{t}}
	imported, err := service.Import(ctx, library.ImportRequest{Library: root, Source: source, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(imported.Path))); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	before := citationTree(t, root)
	var previous CitationResult
	for n := range 3 {
		got, err := service.Citation(ctx, root, imported.AssetID)
		if err != nil || got.Status != "declared_metadata" || got.Schema != "nemalo-citation/1" || len(got.CSLJSON) != 1 || got.Metadata == nil || got.Metadata.ByteStatus != "not_revalidated" || len(got.MetadataSHA256) != 64 {
			t.Fatal(got, err)
		}
		item := got.CSLJSON[0]
		if item.ID != imported.AssetID || item.Type != "document" || item.Title != "Fixture" || item.Language != "en" || item.ISBN != "" || item.DOI != "" {
			t.Fatal(item)
		}
		for _, field := range []string{"author", "issued", "publisher", "edition", "type", "URL", "ISBN", "DOI"} {
			if !slices.Contains(got.UnknownFields, field) {
				t.Fatal("missing unknown field", field, got.UnknownFields)
			}
		}
		encoded, err := json.Marshal(got)
		if err != nil || strings.Contains(string(encoded), source) || strings.Contains(string(encoded), root) {
			t.Fatal("private paths exported", err)
		}
		if n > 0 && !reflect.DeepEqual(previous, got) {
			t.Fatal("stateless citation changed on repeat", previous, got)
		}
		previous = got
	}
	if !reflect.DeepEqual(before, citationTree(t, root)) {
		t.Fatal("citation changed library files")
	}
}

func TestCitationKeepsAmbiguousAndUnknownFieldsOutsideCSLItems(t *testing.T) {
	metadata := library.HoldingMetadata{AssetID: "sha256:" + strings.Repeat("a", 64), Titles: []string{"First", "Second"}, Languages: []string{"en", "en"}, Identities: []library.Identity{{ID: "isbn:9780306406157", Role: "edition"}, {ID: "doi:10.1000/example", Role: "publication_version"}}, Sources: []library.MetadataSource{{Identities: []library.Identity{{ID: "isbn:9780140449136", Role: "edition"}}, Recording: &library.SourceRecording{Readers: []library.Reader{{Name: "A narrator"}}}}}}
	items, fields, unknown := citationFields(metadata)
	if len(items) != 1 || items[0].Title != "" || items[0].ISBN != "" || items[0].Language != "en" || items[0].DOI != "10.1000/example" || !slices.Contains(unknown, "title") || !slices.Contains(unknown, "ISBN") || !slices.Contains(unknown, "author") {
		t.Fatal(items, fields, unknown)
	}
	for _, name := range []string{"title", "ISBN"} {
		found := false
		for _, field := range fields {
			if field.Field == name {
				found = field.Status == "ambiguous" && len(field.Candidates) == 2
			}
		}
		if !found {
			t.Fatal("ambiguous candidates were lost", fields)
		}
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	var raw []map[string]any
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	for field := range raw[0] {
		if !slices.Contains([]string{"id", "type", "title", "language", "ISBN", "DOI"}, field) {
			t.Fatal("nonstandard CSL property", field)
		}
	}
	if strings.Contains(string(encoded), "narrator") || strings.Contains(string(encoded), "unknown") || strings.Contains(string(encoded), "candidates") {
		t.Fatal("sidecar evidence became bibliographic assertion", string(encoded))
	}
}

func TestCitationFailuresReturnNoUsableMetadataAndCreateNothing(t *testing.T) {
	for _, name := range []string{"missing", "uninitialized", "unknown_asset", "invalid_asset", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "library")
			asset := "sha256:" + strings.Repeat("a", 64)
			if name != "missing" {
				if err := os.Mkdir(root, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if name == "unknown_asset" || name == "invalid_asset" {
				if _, err := library.Initialize(context.Background(), root); err != nil {
					t.Fatal(err)
				}
			}
			if name == "invalid_asset" {
				asset = "sha256:bad"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "cancelled" {
				cancel()
			}
			var before map[string]string
			if name != "missing" {
				before = citationTree(t, root)
			}
			got, err := (Service{Scanner: citationNoScan{t}}).Citation(ctx, root, asset)
			if err == nil || got.CSLJSON != nil || got.Metadata != nil || got.MetadataSHA256 != "" || got.Fields != nil || got.UnknownFields != nil || got.Status == "declared_metadata" {
				t.Fatal("failed citation exposed data", got, err)
			}
			if name == "cancelled" && (!errors.Is(err, context.Canceled) || got.Status != "cancelled") {
				t.Fatal(got, err)
			}
			if name == "missing" {
				if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing root created", err)
				}
			} else if !reflect.DeepEqual(before, citationTree(t, root)) {
				t.Fatal("failed citation wrote state")
			}
		})
	}
}

func TestEvidenceJSONDigestPreservesExactNumbersAndOrdersObjects(t *testing.T) {
	a := struct {
		Z uint64         `json:"z"`
		A map[string]int `json:"a"`
	}{18446744073709551615, map[string]int{"second": 2, "first": 1}}
	b := map[string]any{"a": map[string]int{"first": 1, "second": 2}, "z": json.Number("18446744073709551615")}
	one, err := canonicalDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	two, err := canonicalDigest(b)
	if err != nil || one != two {
		t.Fatal("field order or large integer changed identity", one, two, err)
	}
	b["z"] = json.Number("18446744073709551614")
	changed, err := canonicalDigest(b)
	if err != nil || one == changed {
		t.Fatal("distinct large integers lost identity", err)
	}
}
