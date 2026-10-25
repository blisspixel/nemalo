package app

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

// shelf serves fixed Archive items with one public PDF each and counts
// downloads.
type shelf struct {
	mu        sync.Mutex
	bodies    map[string][]byte
	downloads []string
}

func TestCollectionExistingPacketsMustNameTheExactSelection(t *testing.T) {
	ctx := context.Background()
	lib := t.TempDir()
	if _, err := library.Initialize(ctx, lib); err != nil {
		t.Fatal(err)
	}
	for _, recording := range []bool{false, true} {
		for _, receipted := range []bool{false, true} {
			for _, mismatch := range []string{"", "source", "selection", "missing"} {
				t.Run(fmt.Sprintf("recording=%t/receipted=%t/mismatch=%s", recording, receipted, mismatch), func(t *testing.T) {
					item := library.CollectionItem{Source: "archive:demo", File: "book.pdf"}
					if recording {
						item.File, item.Recording = "", "MP3"
					}
					collection := filepath.Join(t.TempDir(), "c.json")
					body, err := json.Marshal(library.Collection{SchemaVersion: 1, Name: "Selection", Items: []library.CollectionItem{item}})
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(collection, body, 0600); err != nil {
						t.Fatal(err)
					}
					intake := t.TempDir()
					packet := filepath.Join(intake, item.Packet())
					if err := os.Mkdir(packet, 0700); err != nil {
						t.Fatal(err)
					}
					source, file, format := item.Source, item.File, item.Recording
					switch mismatch {
					case "source":
						source = "archive:other"
					case "selection":
						if recording {
							format = "VBR MP3"
						} else {
							file = "other.pdf"
						}
					case "missing":
						source = ""
					}
					files := map[string]string{}
					switch {
					case recording && receipted:
						files["recording.json"] = fmt.Sprintf(`{"schema_version":1,"complete":true,"status":"untrusted_intake","source_id":%q,"source_format":%q}`, source, format)
					case recording:
						files["recording-intent.json"] = fmt.Sprintf(`{"request":{"source_id":%q,"source_format":%q},"selection":{"recordings":[{"source_format":%q,"declared_bytes":100}]}}`, source, format, format)
					case receipted:
						files["receipt.json"] = fmt.Sprintf(`{"schema_version":1,"complete":true,"status":"untrusted_intake","intent":{"request":{"source_id":%q,"source_file":%q}}}`, source, file)
					default:
						files["intent.json"] = fmt.Sprintf(`{"schema_version":1,"request":{"source_id":%q,"source_file":%q},"selection":{"files":[{"name":%q,"source_format":"Text PDF","declared_bytes":100}]}}`, source, file, file)
						files["transfer.json"] = fmt.Sprintf(`{"schema_version":1,"status":200,"etag":"\"same\"","strong_etag":true,"source_file":%q}`, file)
						files["content.pdf"] = "partial"
					}
					for name, text := range files {
						if err := os.WriteFile(filepath.Join(packet, name), []byte(text), 0600); err != nil {
							t.Fatal(err)
						}
					}
					service := Service{Providers: map[string]Searcher{}, Scanner: kindScanner{}}
					r, err := service.CollectionAcquire(ctx, CollectionAcquireRequest{Library: lib, Collection: collection, Intake: intake})
					want := StepResume
					if receipted {
						want = StepAwaitingImport
					}
					if mismatch != "" {
						want = StepRetained
					}
					if err != nil || len(r.Steps) != 1 || r.Steps[0].Action != want {
						t.Fatal(r, err)
					}
					if mismatch == "" {
						return
					}
					// Applying a retained step does not evaluate a provider, run a
					// scanner, or modify its bytes, with all external I/O substituted.
					r, err = service.CollectionAcquire(ctx, CollectionAcquireRequest{Library: lib, Collection: collection, Intake: intake, Apply: true})
					if err != nil || r.Complete || r.Steps[0].Outcome != "not_started" || !strings.Contains(r.Steps[0].Detail, "exact collection selection") {
						t.Fatal(r, err)
					}
					for name, text := range files {
						got, err := os.ReadFile(filepath.Join(packet, name))
						if err != nil || string(got) != text {
							t.Fatal("retained packet changed", name, err)
						}
					}
				})
			}
		}
	}
}

func (s *shelf) Search(context.Context, string, int, int) (discovery.Page, error) {
	return discovery.Page{}, nil
}

func (s *shelf) Evaluate(_ context.Context, id string) (discovery.Evaluation, error) {
	body, ok := s.bodies[id]
	if !ok {
		return discovery.Evaluation{}, errors.New("no such item")
	}
	n, sum := int64(len(body)), md5.Sum(body)
	item := strings.TrimPrefix(id, "archive:")
	return discovery.Evaluation{Complete: true, ID: id, Source: "archive", Access: "no_item_restriction_declared", Title: []string{item}, Files: []discovery.OfferedFile{{
		Name: item + ".pdf", Format: "Text PDF", Bytes: &n, MD5: hex.EncodeToString(sum[:]), Access: "public_file_candidate", DownloadURL: "https://archive.org/download/" + item + "/" + item + ".pdf",
	}}}, nil
}

func (s *shelf) Download(_ context.Context, f discovery.OfferedFile) (*http.Response, error) {
	s.mu.Lock()
	s.downloads = append(s.downloads, f.Name)
	s.mu.Unlock()
	body := s.bodies["archive:"+strings.TrimSuffix(f.Name, ".pdf")]
	return &http.Response{StatusCode: 200, ContentLength: int64(len(body)), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
}

// A collection acquires only what the library does not hold, into packets
// named by item, and a second run after ingest has nothing to do; a
// retained attempt is left as it is.
func TestCollectionAcquiresOnlyWhatIsMissing(t *testing.T) {
	ctx := context.Background()
	lib := t.TempDir()
	if _, err := library.Initialize(ctx, lib); err != nil {
		t.Fatal(err)
	}
	provider := &shelf{bodies: map[string][]byte{"archive:one": pdftest.Pages("", "One."), "archive:two": pdftest.Pages("", "Two."), "archive:three": pdftest.Pages("", "Three.")}}
	s := Service{Providers: map[string]Searcher{"archive": provider}}
	collection := filepath.Join(t.TempDir(), "c.json")
	write := func(body string) {
		if err := os.WriteFile(collection, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"schema_version":1,"name":"Shelf","items":[{"source":"archive:one","file":"one.pdf"},{"source":"archive:two","file":"two.pdf","note":"second"},{"source":"archive:three","file":"three.pdf"}]}`)
	intake := filepath.Join(t.TempDir(), "intake")
	retained := filepath.Join(intake, library.CollectionItem{Source: "archive:three", File: "three.pdf"}.Packet())
	if err := os.MkdirAll(retained, 0o700); err != nil {
		t.Fatal(err)
	}
	preview, err := s.CollectionAcquire(ctx, CollectionAcquireRequest{Library: lib, Collection: collection, Intake: intake, NoScan: true})
	if err != nil || preview.Applied || len(preview.Steps) != 3 || preview.Steps[2].Action != StepRetained || len(provider.downloads) != 0 {
		t.Fatal(preview, err)
	}
	applied, err := s.CollectionAcquire(ctx, CollectionAcquireRequest{Library: lib, Collection: collection, Intake: intake, Apply: true, NoScan: true})
	if err != nil || applied.Complete || len(provider.downloads) != 2 || applied.Steps[0].Outcome != "untrusted_intake" || applied.Steps[2].Outcome != "not_started" {
		t.Fatalf("%+v %v %v", applied.Steps, provider.downloads, err)
	}
	if err := os.Remove(retained); err != nil {
		t.Fatal(err)
	}
	if r, err := library.Ingest(ctx, library.IngestRequest{Library: lib, Folder: intake, Apply: true}); err != nil {
		t.Fatal(r, err)
	}
	status, err := s.CollectionStatus(ctx, lib, collection)
	if err != nil || status.InReview != 2 || status.NotHeld != 1 || status.Items[0].Item.Source != "archive:one" || len(status.Items[0].Holdings) != 1 {
		t.Fatalf("%+v %v", status, err)
	}
	// Only the missing item is fetched; the held ones are not touched.
	again, err := s.CollectionAcquire(ctx, CollectionAcquireRequest{Library: lib, Collection: collection, Intake: intake, Apply: true, NoScan: true})
	if err != nil || !again.Complete || len(again.Steps) != 1 || len(provider.downloads) != 3 {
		t.Fatalf("%+v %v %v", again.Steps, provider.downloads, err)
	}
	// A packet awaiting ingest is not fetched again.
	waiting, err := s.CollectionAcquire(ctx, CollectionAcquireRequest{Library: lib, Collection: collection, Intake: intake, Apply: true, NoScan: true})
	if err != nil || len(waiting.Steps) != 1 || waiting.Steps[0].Action != StepAwaitingImport || len(provider.downloads) != 3 {
		t.Fatalf("%+v %v", waiting.Steps, err)
	}
	// A total budget smaller than an item stops it before any request.
	write(`{"schema_version":1,"name":"Shelf","items":[{"source":"archive:one","file":"one.pdf"},{"source":"archive:four","file":"four.pdf"}]}`)
	provider.bodies["archive:four"] = pdftest.Pages("", "Four, a longer document than the budget allows.")
	small, err := s.CollectionAcquire(ctx, CollectionAcquireRequest{Library: lib, Collection: collection, Intake: intake, Apply: true, NoScan: true, MaxTotalBytes: 10})
	if err != nil || small.Complete || len(provider.downloads) != 3 {
		t.Fatalf("%+v %v", small.Steps, err)
	}
	for _, bad := range []CollectionAcquireRequest{{Library: lib, Collection: collection, Intake: "relative"}, {Library: lib, Collection: collection, Intake: filepath.Join(lib, "intake")}} {
		if _, err := s.CollectionAcquire(ctx, bad); err == nil {
			t.Fatal("accepted", bad.Intake)
		}
	}
}
