package library

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func rewriteImportOperations(t *testing.T, root string, edit func(*operation)) {
	t.Helper()
	name := filepath.Join(root, ".nemalo", operationsName)
	var out bytes.Buffer
	for _, line := range bytes.Split(bytes.TrimSpace(mustRead(t, name)), []byte{'\n'}) {
		var event operation
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		edit(&event)
		b, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		out.Write(b)
		out.WriteByte('\n')
	}
	if err := os.WriteFile(name, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

// Include directories as well as every file, so refusals cannot leave an
// import intent, a store directory, a temporary copy, or changed bytes behind.
func importTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			out[rel] = "directory"
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func proposalPacket(t *testing.T) (string, []byte) {
	t.Helper()
	body := mp3Track(40)
	receipt := audioReceipt(t, body, "demo", "part.mp3", "7", "demo", map[string]any{
		"source_format": "MP3", "position": 1, "tracks": 2, "title": "Chapter one", "declared_seconds": 2,
		"readers": []map[string]string{{"librivox_reader_id": "1", "name": "Reader one"}},
	})
	var raw map[string]any
	if err := json.Unmarshal(receipt, &raw); err != nil {
		t.Fatal(err)
	}
	raw["final_url"] = "https://example.org/part.mp3"
	selection := raw["intent"].(map[string]any)["selection"].(map[string]any)
	selection["rights_statements"] = []string{"Declared public domain"}
	selection["declared_license_urls"] = []string{"https://example.org/license"}
	receipt, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	packet := t.TempDir()
	writeTree(t, packet, map[string][]byte{"content.mp3": body, "receipt.json": receipt})
	return packet, receipt
}

func editProposalReceipt(t *testing.T, packet string, original []byte, edit func(map[string]any)) {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(original, &raw); err != nil {
		t.Fatal(err)
	}
	edit(raw)
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, packet, map[string][]byte{"receipt.json": b})
}

func TestImportPlanBindsNormalizedProvenanceBeforeAnyWrite(t *testing.T) {
	edits := map[string]func(map[string]any){
		"source": func(r map[string]any) {
			r["intent"].(map[string]any)["request"].(map[string]any)["source_id"] = "archive:changed"
		},
		"file": func(r map[string]any) {
			r["intent"].(map[string]any)["request"].(map[string]any)["source_file"] = "other.mp3"
		},
		"url": func(r map[string]any) { r["final_url"] = "https://example.org/other.mp3" },
		"rights": func(r map[string]any) {
			r["intent"].(map[string]any)["selection"].(map[string]any)["rights_statements"] = []string{"Changed declaration"}
		},
		"license": func(r map[string]any) {
			r["intent"].(map[string]any)["selection"].(map[string]any)["declared_license_urls"] = []string{"https://example.org/other-license"}
		},
		"position": func(r map[string]any) {
			r["intent"].(map[string]any)["request"].(map[string]any)["recording"].(map[string]any)["position"] = 2
		},
		"count": func(r map[string]any) {
			r["intent"].(map[string]any)["request"].(map[string]any)["recording"].(map[string]any)["tracks"] = 3
		},
		"reader": func(r map[string]any) {
			r["intent"].(map[string]any)["request"].(map[string]any)["recording"].(map[string]any)["readers"] = []map[string]string{{"librivox_reader_id": "2", "name": "Reader two"}}
		},
		"narration": func(r map[string]any) {
			r["intent"].(map[string]any)["selection"].(map[string]any)["librivox"].(map[string]any)["project_id"] = "8"
		},
		"identity": func(r map[string]any) {
			r["intent"].(map[string]any)["selection"].(map[string]any)["declared_identifiers"] = []map[string]string{{"field": "openlibrary_work", "value": "OL1W"}}
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			if _, err := Initialize(ctx, root); err != nil {
				t.Fatal(err)
			}
			packet, original := proposalPacket(t)
			request := ImportRequest{Library: root, Source: packet}
			preview, err := Import(ctx, request)
			if err != nil || preview.Holding == nil || preview.SourceEvidence == nil {
				t.Fatal(preview, err)
			}
			before := importTree(t, root)
			editProposalReceipt(t, packet, original, edit)
			fresh, err := Import(ctx, request)
			if err != nil || fresh.Plan == preview.Plan {
				t.Fatal("metadata did not change proposal", fresh, err)
			}
			request.Apply, request.Plan = true, preview.Plan
			refused, err := Import(ctx, request)
			if !errors.Is(err, ErrPlanChanged) || refused.Applied || refused.Plan != "" || refused.Holding != nil || refused.SourceEvidence != nil {
				t.Fatal(refused, err)
			}
			if !reflect.DeepEqual(before, importTree(t, root)) {
				t.Fatal("stale plan changed library")
			}
			request.Plan = fresh.Plan
			applied, err := Import(ctx, request)
			if err != nil || !applied.Applied || !reflect.DeepEqual(fresh.Holding, applied.Holding) {
				t.Fatal(applied, err)
			}
			b := mustRead(t, filepath.Join(root, ".nemalo", holdingsName))
			var stored Holding
			if err := json.Unmarshal(bytes.TrimSpace(b), &stored); err != nil || !reflect.DeepEqual(stored, *fresh.Holding) {
				t.Fatal("proposal differs from stored record", stored, err)
			}
			before = importTree(t, root)
			repeated, err := Import(ctx, request)
			if err != nil || !repeated.AlreadyHeld || repeated.Plan != fresh.Plan {
				t.Fatal(repeated, err)
			}
			if !reflect.DeepEqual(before, importTree(t, root)) {
				t.Fatal("repeat rewrote state")
			}
		})
	}
}

func TestImportPlanOmitsUnstoredReceiptFields(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	packet, original := proposalPacket(t)
	editProposalReceipt(t, packet, original, func(r map[string]any) {
		r["intent"].(map[string]any)["selection"].(map[string]any)["rights_statements"] = []string{strings.Repeat("a", 2001)}
	})
	request := ImportRequest{Library: root, Source: packet}
	a, err := Import(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	editProposalReceipt(t, packet, original, func(r map[string]any) {
		r["acquired_at"] = "different unrecorded timestamp"
		s := r["intent"].(map[string]any)["selection"].(map[string]any)
		s["rights_statements"] = []string{"Declared public domain", strings.Repeat("x", 2001)}
		s["declared_identifiers"] = []map[string]string{{"field": "unknown", "value": "ignored"}}
	})
	b, err := Import(ctx, request)
	if err != nil || b.Plan != a.Plan || !reflect.DeepEqual(a.Holding, b.Holding) {
		t.Fatal(a, b, err)
	}
	request.Apply, request.Plan = true, a.Plan
	if r, err := Import(ctx, request); err != nil || !r.Applied {
		t.Fatal(r, err)
	}
}

func TestInterruptedImportRetainsItsOriginalProvenance(t *testing.T) {
	for _, stage := range []string{"intent", "copied", "linked", "recorded"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			if _, err := Initialize(ctx, root); err != nil {
				t.Fatal(err)
			}
			packet, original := proposalPacket(t)
			request := ImportRequest{Library: root, Source: packet}
			preview, err := Import(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			request.Apply, request.Plan = true, preview.Plan
			stop := errors.New("simulated interruption")
			if _, err := importLibrary(ctx, request, func(at string) error {
				if at == stage {
					return stop
				}
				return nil
			}); !errors.Is(err, stop) {
				t.Fatal(err)
			}
			before := importTree(t, root)
			editProposalReceipt(t, packet, original, func(r map[string]any) { r["final_url"] = "https://example.org/changed" })
			r, err := Import(ctx, request)
			if !errors.Is(err, ErrPlanChanged) || r.Applied || r.Plan != "" {
				t.Fatal(r, err)
			}
			request.Plan = ""
			r, err = Import(ctx, request)
			if !errors.Is(err, ErrImportEvidenceChanged) || r.Applied || r.Plan != "" {
				t.Fatal(r, err)
			}
			if !reflect.DeepEqual(before, importTree(t, root)) {
				t.Fatal("refused recovery changed library")
			}
			writeTree(t, packet, map[string][]byte{"receipt.json": original})
			request.Plan = preview.Plan
			r, err = Import(ctx, request)
			if err != nil || !r.Resumed || !r.Applied || !reflect.DeepEqual(preview.Holding, r.Holding) {
				t.Fatal(r, err)
			}
		})
	}
}

func TestLegacyCompletedImportsRemainReadableButPendingAreRetained(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "pending"}[pending], func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			if _, err := Initialize(ctx, root); err != nil {
				t.Fatal(err)
			}
			packet, _ := proposalPacket(t)
			request := ImportRequest{Library: root, Source: packet, Apply: true}
			stop := errors.New("interruption")
			_, err := importLibrary(ctx, request, func(at string) error {
				if pending && at == "intent" {
					return stop
				}
				return nil
			})
			if pending && !errors.Is(err, stop) || !pending && err != nil {
				t.Fatal(err)
			}
			rewriteImportOperations(t, root, func(e *operation) { e.ImportSourceSHA256, e.ImportHoldingSHA256 = "", "" })
			if _, err := LibraryStatus(ctx, root); err != nil {
				t.Fatal("legacy state rejected", err)
			}
			before := importTree(t, root)
			if pending {
				previewRequest := request
				previewRequest.Apply = false
				if _, err := Import(ctx, previewRequest); !errors.Is(err, ErrLegacyPendingImport) {
					t.Fatal("legacy preview did not explain recovery", err)
				}
			}
			r, err := Import(ctx, request)
			if pending {
				if !errors.Is(err, ErrLegacyPendingImport) || r.Applied {
					t.Fatal(r, err)
				}
			} else if err != nil || !r.AlreadyHeld {
				t.Fatal(r, err)
			}
			if !reflect.DeepEqual(before, importTree(t, root)) {
				t.Fatal("legacy state rewritten")
			}
		})
	}
}

func TestImportBindingRejectsMalformedPairsAndChangedHoldings(t *testing.T) {
	for _, kind := range []string{"source_missing", "holding_missing", "bad_hash", "different_pair", "rescan", "changed_holding", "unknown_field"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			if _, err := Initialize(ctx, root); err != nil {
				t.Fatal(err)
			}
			packet, _ := proposalPacket(t)
			if _, err := Import(ctx, ImportRequest{Library: root, Source: packet, Apply: true}); err != nil {
				t.Fatal(err)
			}
			if kind == "changed_holding" {
				p := filepath.Join(root, ".nemalo", holdingsName)
				b := mustRead(t, p)
				b = bytes.ReplaceAll(b, []byte("Declared public domain"), []byte("Changed declaration"))
				if err := os.WriteFile(p, b, 0600); err != nil {
					t.Fatal(err)
				}
			} else if kind == "unknown_field" {
				p := filepath.Join(root, ".nemalo", operationsName)
				b := bytes.ReplaceAll(mustRead(t, p), []byte(`"import_source_sha256"`), []byte(`"unknown_hash"`))
				if err := os.WriteFile(p, b, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				rewriteImportOperations(t, root, func(e *operation) {
					switch kind {
					case "source_missing":
						e.ImportSourceSHA256 = ""
					case "holding_missing":
						e.ImportHoldingSHA256 = ""
					case "bad_hash":
						e.ImportSourceSHA256 = strings.Repeat("G", 64)
					case "different_pair":
						if e.Event == "import_completed" {
							e.ImportSourceSHA256 = strings.Repeat("a", 64)
						}
					case "rescan":
						e.Event = strings.Replace(e.Event, "import_", "rescan_", 1)
					}
				})
			}
			before := importTree(t, root)
			if _, err := LibraryStatus(ctx, root); !errors.Is(err, ErrStateReview) {
				t.Fatal("invalid bound state accepted", kind, err)
			}
			if !reflect.DeepEqual(before, importTree(t, root)) {
				t.Fatal("invalid state rewritten")
			}
		})
	}
}

func TestImportPreviewProjectsPendingRescanAndBindsExistingDisposition(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUBParagraph(t, t.TempDir(), "Rescan preview")
	request := ImportRequest{Library: root, Source: source, Apply: true}
	first, err := Import(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Apply = false
	old, err := Import(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := openControl(root, true)
	if err != nil {
		t.Fatal(err)
	}
	events, _, err := lockedManaged(c, first.LibraryID)
	if err != nil {
		t.Fatal(err)
	}
	intent := operation{SchemaVersion: 1, Sequence: len(events) + 1, LibraryID: first.LibraryID, OperationID: newStateID("operation"), Event: "rescan_intent", Source: first.Path, AssetID: first.AssetID, Format: first.Format, Bytes: first.Bytes, Status: "checked", Reason: "checks_and_scan_recorded", At: time.Now().UTC().Format(time.RFC3339Nano)}
	err = appendCanonical(ctx, c, operationsName, maxOperations, intent)
	closeErr := c.close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
	before := importTree(t, root)
	preview, err := Import(ctx, request)
	if err != nil || preview.Status != "checked" || preview.Antivirus == "no_detections_reported" || preview.Holding == nil || preview.Holding.Status != "checked" || preview.Plan == old.Plan {
		t.Fatal(preview, err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("preview completed rescan")
	}
	request.Apply, request.Plan = true, old.Plan
	refused, err := Import(ctx, request)
	if !errors.Is(err, ErrPlanChanged) || refused.Applied || !refused.RecoveredRescan {
		t.Fatal(refused, err)
	}
	events, err = parseOperations(mustRead(t, filepath.Join(root, ".nemalo", operationsName)), first.LibraryID)
	if err != nil || len(events) != 4 || events[3].Event != "rescan_completed" {
		t.Fatal("refusal started import work", events, err)
	}
	before = importTree(t, root)
	request.Plan = preview.Plan
	applied, err := Import(ctx, request)
	if err != nil || !applied.AlreadyHeld || applied.RecoveredRescan || !reflect.DeepEqual(preview.Holding, applied.Holding) {
		t.Fatal(applied, err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("repeat wrote after recovery")
	}
}

func TestImportArchivePlanAndRecoveryUseStableMemberLabel(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	a := writeEPUBParagraph(t, t.TempDir(), "Staged member")
	b := filepath.Join(t.TempDir(), "new.epub")
	if err := os.WriteFile(b, mustRead(t, a), 0600); err != nil {
		t.Fatal(err)
	}
	request := ImportRequest{Library: root, Source: a, Label: filepath.Join(t.TempDir(), "books.zip") + "!/book.epub"}
	preview, err := Import(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Source = b
	other, err := Import(ctx, request)
	if err != nil || other.Plan != preview.Plan || other.SourceEvidence.Kind != "archive_member" || other.SourceEvidence.Path != request.Label {
		t.Fatal(other, err)
	}
	request.Source = a
	request.Apply, request.Plan = true, preview.Plan
	stop := errors.New("interruption")
	if _, err := importLibrary(ctx, request, func(at string) error {
		if at == "recorded" {
			return stop
		}
		return nil
	}); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	request.Source = b
	resumed, err := Import(ctx, request)
	if err != nil || !resumed.Applied || !resumed.Resumed || !reflect.DeepEqual(preview.Holding, resumed.Holding) {
		t.Fatal(resumed, err)
	}
}

// Existing source declarations remain historical. The incoming declaration is
// still preview-bound, even when the merge deliberately retains older fields.
func TestImportProposalDistinguishesIncomingFromRetainedProvenance(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	packet, original := proposalPacket(t)
	request := ImportRequest{Library: root, Source: packet, Apply: true}
	first, err := Import(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Apply = false
	old, err := Import(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	editProposalReceipt(t, packet, original, func(r map[string]any) { r["final_url"] = "https://example.org/new-declaration" })
	fresh, err := Import(ctx, request)
	if err != nil || fresh.Plan == old.Plan || fresh.SourceEvidence.DeclaredURL == first.SourceEvidence.DeclaredURL || !reflect.DeepEqual(fresh.Holding, first.Holding) {
		t.Fatal(fresh, err)
	}
	before := importTree(t, root)
	request.Apply, request.Plan = true, old.Plan
	if r, err := Import(ctx, request); !errors.Is(err, ErrPlanChanged) || r.Applied {
		t.Fatal(r, err)
	}
	request.Plan = fresh.Plan
	if r, err := Import(ctx, request); err != nil || !r.AlreadyHeld || !reflect.DeepEqual(r.Holding, first.Holding) {
		t.Fatal(r, err)
	}
	if !reflect.DeepEqual(before, importTree(t, root)) {
		t.Fatal("retained provenance was replaced")
	}
}

// A previous extractor can have recorded different bibliographic facts for
// the same bytes. A no-op must not return an unwritten replacement holding.
func TestImportWritesTheFullPreviewedHoldingWhenExtractedMetadataChanges(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	source := writeEPUBParagraph(t, t.TempDir(), "Updated extraction")
	request := ImportRequest{Library: root, Source: source, Apply: true}
	first, err := Import(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	old := *first.Holding
	old.Titles = []string{"Previously extracted title"}
	old.Languages = []string{"fr"}
	b, err := recordBytes(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".nemalo", holdingsName), b, 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := canonicalDigest(old)
	if err != nil {
		t.Fatal(err)
	}
	rewriteImportOperations(t, root, func(e *operation) { e.ImportHoldingSHA256 = digest })
	request.Apply = false
	preview, err := Import(ctx, request)
	if err != nil || reflect.DeepEqual(preview.Holding.Titles, old.Titles) {
		t.Fatal(preview, err)
	}
	request.Apply, request.Plan = true, preview.Plan
	applied, err := Import(ctx, request)
	if err != nil || !applied.Applied {
		t.Fatal(applied, err)
	}
	state, err := LibraryStatus(ctx, root)
	if err != nil {
		t.Fatal(state, err)
	}
	lines, err := parseHoldings(mustRead(t, filepath.Join(root, ".nemalo", holdingsName)), first.LibraryID)
	if err != nil || !reflect.DeepEqual(latestHoldings(lines)[first.AssetID], *preview.Holding) {
		t.Fatal("previewed metadata was not stored", lines, preview.Holding, err)
	}
}
