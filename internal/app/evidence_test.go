package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/audio/audiotest"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/decode"
	"github.com/blisspixel/nemalo/internal/decode/decodetest"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

func evidenceHolding(t *testing.T, name string, data []byte) (content.Request, string) {
	t.Helper()
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	held, err := (Service{Scanner: citationNoScan{t}}).Import(context.Background(), library.ImportRequest{Library: root, Source: source, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	return content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: held.AssetID, MaxBytes: 4096, Evidence: true}, filepath.Join(root, filepath.FromSlash(held.Path))
}

func evidenceBook(t *testing.T) []byte {
	t.Helper()
	files := []struct{ name, body string }{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Source fixture</title><language>en</language></metadata><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/><item id="b" href="b.svg" media-type="image/svg+xml"/><item id="c" href="c.xhtml" media-type="application/xhtml+xml"/><item id="n" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest><spine><itemref idref="a"/><itemref idref="b"/><itemref idref="c"/></spine></package>`},
		{"a.xhtml", `<html><body><p id="start">Café αβ. Early claim.</p><table id="omitted"><tr><td>Excluded table.</td></tr></table><p><span epub:type="pagebreak" xmlns:epub="http://www.idpf.org/2007/ops" id="p1"/>Later qualification.</p></body></html>`},
		{"b.svg", `<svg xmlns="http://www.w3.org/2000/svg"><text>Not silently read.</text></svg>`},
		{"c.xhtml", `<html><body><p id="later">Distinct final unit.</p></body></html>`},
		{"nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="a.xhtml#start">Beginning</a></li><li><a href="c.xhtml#later">Later</a></li></ol></nav><nav epub:type="page-list"><ol><li><a href="a.xhtml#p1">1</a></li></ol></nav></body></html>`},
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, file := range files {
		w, err := z.CreateHeader(&zip.FileHeader{Name: file.name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(file.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func noEvidencePayload(t *testing.T, result content.Result, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("failed evidence read succeeded", result)
	}
	want := content.Result{SchemaVersion: result.SchemaVersion, Status: result.Status, AssetID: result.AssetID, Limitations: result.Limitations}
	if !reflect.DeepEqual(result, want) || result.Status == "complete_range" || result.Status == "partial_range" {
		t.Fatal("failed evidence leaked payload or successful status", result, err)
	}
}

func TestEvidenceExactTextTypedRangesAndReplayKeepDefaultResults(t *testing.T) {
	for _, rep := range []string{"", "epub-text/1", "epub-text/2", "epub-structure/1", "epub-nav/1", "epub-page-list/1", "pdf-text/1"} {
		t.Run(rep, func(t *testing.T) {
			data, name := evidenceBook(t), "book.epub"
			if rep == "epub-text/1" || rep == "epub-structure/1" {
				// These earlier representations preserve their stricter refusal of
				// tables and unsupported spine members. Exercise their supported
				// range independently of the newer gap-reporting fixture.
				data = epubWithText("Café αβ. Early claim. Later qualification.")
			}
			if rep == "pdf-text/1" {
				data, name = pdftest.Pages("", "A defined method and its limits.", "Unselected page."), "paper.pdf"
			}
			req, _ := evidenceHolding(t, name, data)
			req.Representation, req.MaxBytes = rep, 12
			before := citationTree(t, req.Root)
			s := Service{Scanner: citationNoScan{t}}
			original := req
			original.Evidence = false
			legacy, err := s.Content(context.Background(), original)
			if err != nil {
				t.Fatal(legacy, err)
			}
			got, err := s.Content(context.Background(), req)
			if err != nil || got.Evidence == nil {
				t.Fatal(got, err)
			}
			e := got.Evidence
			if e.Schema != "nemalo-evidence/1" || e.Kind != "source_text" || e.Quotation != got.Text || e.Quotation == "" || !reflect.DeepEqual(e.Locator, got.Locator) || e.Locator.OffsetUnit != got.Locator.OffsetUnit || len(e.Coverage.ReturnedUnits) != 1 || e.Coverage.ReturnedUnits[0] != got.Locator.Unit || e.Coverage.HasContinuation != (got.Continuation != "") || e.Coverage.EndOfSource != got.EndOfSource || len(e.Reference) > 2048 {
				t.Fatal(e)
			}
			if rep == "" && got.Text != "Café αβ. " {
				t.Fatal("quotation did not preserve UTF-8 bytes", got.Text)
			}
			got.Evidence = nil
			if !reflect.DeepEqual(got, legacy) {
				t.Fatal("opt-in changed the original retrieval", got, legacy)
			}
			req.Evidence, req.EvidenceReference = false, e.Reference
			replay, err := s.Content(context.Background(), req)
			if err != nil || !reflect.DeepEqual(replay.Evidence, e) {
				t.Fatal("exact replay failed", replay, err)
			}
			req.MaxBytes++
			failed, err := s.Content(context.Background(), req)
			noEvidencePayload(t, failed, err)
			if failed.Status != "stale_reference" {
				t.Fatal(failed.Status, err)
			}
			if !reflect.DeepEqual(before, citationTree(t, req.Root)) {
				t.Fatal("evidence changed files")
			}
		})
	}
}

func TestEvidenceGapsContinuationsUnsupportedAndEmptyRangesStayExplicit(t *testing.T) {
	req, _ := evidenceHolding(t, "book.epub", evidenceBook(t))
	s := Service{Scanner: citationNoScan{t}}
	got, err := s.Content(context.Background(), req)
	if err != nil || got.Evidence == nil || len(got.Gaps) != 1 || !reflect.DeepEqual(got.Evidence.Coverage.Gaps, got.Gaps) || !reflect.DeepEqual(got.Evidence.Coverage.UnsupportedUnits, []int{1}) || got.EndOfSource || len(got.Evidence.Coverage.ReturnedUnits) != 1 {
		t.Fatal(got, err)
	}
	first := got.Evidence.Reference
	req.Cursor = got.Continuation
	failed, err := s.Content(context.Background(), req)
	noEvidencePayload(t, failed, err)
	if failed.Status != "unsupported_extraction" {
		t.Fatal("unsupported unit was skipped", failed, err)
	}
	req.Cursor, req.Unit, req.EvidenceReference = "", 2, first
	failed, err = s.Content(context.Background(), req)
	noEvidencePayload(t, failed, err)
	if failed.Status != "stale_reference" {
		t.Fatal(failed, err)
	}
	req.EvidenceReference = ""
	last, err := s.Content(context.Background(), req)
	if err != nil || last.Evidence == nil || !last.EndOfSource || !reflect.DeepEqual(last.Evidence.Coverage.ReturnedUnits, []int{2}) {
		t.Fatal("end marker became whole-work coverage", last, err)
	}
	// A larger request producing the same bytes still names a different bound.
	req.EvidenceReference, req.MaxBytes = last.Evidence.Reference, req.MaxBytes+1
	failed, err = s.Content(context.Background(), req)
	noEvidencePayload(t, failed, err)
}

func TestEvidenceOriginalAudioHashesDeliveredBytesAndKeepsClockUnits(t *testing.T) {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x40})
	for _, rep := range []string{"mpeg-audio/1", content.MP4PacketRepresentation} {
		t.Run(rep, func(t *testing.T) {
			name, data, unit := "track.mp3", bytes.Repeat(frame, 12), "encoded_samples"
			if rep == content.MP4PacketRepresentation {
				name, data, unit = "track.m4b", audiotest.MP4(4), "mp4_packets"
			}
			req, _ := evidenceHolding(t, name, data)
			req.Representation, req.MaxBytes = rep, 1000
			s := Service{Scanner: citationNoScan{t}, PCMDecoder: func(context.Context, string, string, int64, decode.PCMRequest) (decode.PCMRange, error) {
				t.Fatal("original bytes invoked a decoder")
				return decode.PCMRange{}, nil
			}}
			got, err := s.Content(context.Background(), req)
			if err != nil || got.Resource == nil || got.Evidence == nil {
				t.Fatal(got, err)
			}
			hash := sha256.Sum256(got.Resource.Data)
			e := got.Evidence
			if e.Kind != "original_audio_bytes" || e.Quotation != "" || e.Locator.OffsetUnit != unit || e.DeliveredSHA256 != hex.EncodeToString(hash[:]) || e.DeliveredBytes != len(got.Resource.Data) || e.PCMRange != nil {
				t.Fatal(e)
			}
			req.EvidenceReference = e.Reference
			replayed, err := s.Content(context.Background(), req)
			if err != nil || !reflect.DeepEqual(replayed, got) {
				t.Fatal(replayed, err)
			}
		})
	}
}

func TestEvidenceDecodedReplayBindsFullOutputAndRequiresEveryCallPermission(t *testing.T) {
	data := audiotest.MP4(4)
	req, _ := evidenceHolding(t, "track.m4b", data)
	req.Representation, req.Decode, req.MaxBytes = content.PCMRepresentation, true, 1044
	fake := decodetest.NewPCM(data)
	s := Service{Scanner: citationNoScan{t}, PCMDecoder: fake.Decode}
	before := citationTree(t, req.Root)
	got, err := s.Content(context.Background(), req)
	if err != nil || got.Evidence == nil || got.Evidence.Kind != "decoded_audio_samples" || got.Evidence.Locator != nil || got.Evidence.PCMRange.OffsetUnit != "decoder_output_samples" || got.Evidence.PCMRange.EndSample != "500" {
		t.Fatal(got, err)
	}
	req.Evidence, req.EvidenceReference = false, got.Evidence.Reference
	if replay, err := s.Content(context.Background(), req); err != nil || !reflect.DeepEqual(got, replay) {
		t.Fatal(replay, err)
	}
	for _, name := range []string{"no_permission", "metadata_before_decode", "holding_before_decode", "library_before_decode", "extractor_mismatch", "retrieval_mismatch", "changed_full_output", "changed_version", "cancel_during_decode", "writer_exclusion"} {
		t.Run(name, func(t *testing.T) {
			read := req
			service := s
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := fake.Calls.Load()
			switch name {
			case "no_permission":
				read.Decode = false
			case "metadata_before_decode", "holding_before_decode", "library_before_decode", "extractor_mismatch", "retrieval_mismatch":
				ref, err := parseEvidenceReference(read.EvidenceReference)
				if err != nil {
					t.Fatal(err)
				}
				switch name {
				case "metadata_before_decode":
					ref.MetadataSHA256 = strings.Repeat("a", 64)
				case "holding_before_decode":
					ref.HoldingSHA256 = strings.Repeat("a", 64)
				case "library_before_decode":
					ref.LibraryID = "library:" + strings.Repeat("a", 32)
				case "extractor_mismatch":
					ref.Extractor = "unknown-extractor/2"
				case "retrieval_mismatch":
					ref.RetrievalSHA256 = strings.Repeat("a", 64)
				}
				read.EvidenceReference = encodeEvidenceReference(ref)
			case "changed_full_output":
				fake.Data[len(fake.Data)-1] ^= 1
				defer func() { fake.Data[len(fake.Data)-1] ^= 1 }()
			case "changed_version":
				service.PCMDecoder = func(ctx context.Context, p, f string, n int64, q decode.PCMRequest) (decode.PCMRange, error) {
					out, err := fake.Decode(ctx, p, f, n, q)
					out.Measurement.Version = "synthetic/2"
					return out, err
				}
			case "cancel_during_decode":
				service.PCMDecoder = func(ctx context.Context, p, f string, n int64, q decode.PCMRequest) (decode.PCMRange, error) {
					out, err := fake.Decode(ctx, p, f, n, q)
					cancel()
					return out, err
				}
			case "writer_exclusion":
				service.PCMDecoder = func(ctx context.Context, p, f string, n int64, q decode.PCMRequest) (decode.PCMRange, error) {
					if _, err := library.Initialize(ctx, read.Root); !errors.Is(err, library.ErrBusy) {
						t.Fatal("evidence did not retain reader lock", err)
					}
					return fake.Decode(ctx, p, f, n, q)
				}
			}
			result, err := service.Content(ctx, read)
			if name == "writer_exclusion" {
				if err != nil || !reflect.DeepEqual(result, got) {
					t.Fatal(result, err)
				}
				return
			}
			noEvidencePayload(t, result, err)
			if name == "no_permission" && fake.Calls.Load() != calls {
				t.Fatal("reference granted decode authority")
			}
			if strings.HasSuffix(name, "before_decode") && (fake.Calls.Load() != calls || result.Status != "stale_reference") {
				t.Fatal("stale metadata reached decoder", result, err)
			}
			if strings.HasSuffix(name, "mismatch") && result.Status != "stale_reference" {
				t.Fatal("forged binding accepted", result, err)
			}
			if (name == "changed_full_output" || name == "changed_version") && result.Status != "stale_reference" {
				t.Fatal(result, err)
			}
			if name == "cancel_during_decode" && result.Status != "cancelled" {
				t.Fatal(result, err)
			}
		})
	}
	if !reflect.DeepEqual(before, citationTree(t, req.Root)) {
		t.Fatal("decoded evidence wrote library state")
	}
}

func TestEvidenceFailuresDiscardPayloadAndNeverInitialize(t *testing.T) {
	for _, name := range []string{"missing_root", "snapshot", "wrong_root", "unknown_asset", "malformed_token", "wrong_asset", "metadata_mismatch", "torn_journal", "missing_body", "changed_body", "cancelled", "units", "source", "source_cursor", "find", "resource"} {
		t.Run(name, func(t *testing.T) {
			req, body := evidenceHolding(t, "book.epub", epubWithText("Exact source with enough bytes for continuation."))
			s := Service{Scanner: citationNoScan{t}}
			first, err := s.Content(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			req.EvidenceReference = first.Evidence.Reference
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "missing_root":
				req.Catalog = filepath.Join(t.TempDir(), "missing")
				req.Root = req.Catalog
			case "snapshot":
				req.Catalog = body
				req.Root = body
			case "wrong_root":
				req.Root = t.TempDir()
			case "unknown_asset":
				req.AssetID = "sha256:" + strings.Repeat("a", 64)
				req.EvidenceReference = ""
			case "malformed_token":
				req.EvidenceReference = "nemaloevidence1:bad"
			case "wrong_asset":
				req.AssetID = "sha256:" + strings.Repeat("a", 64)
			case "metadata_mismatch":
				ref, err := parseEvidenceReference(req.EvidenceReference)
				if err != nil {
					t.Fatal(err)
				}
				ref.MetadataSHA256 = strings.Repeat("a", 64)
				req.EvidenceReference = encodeEvidenceReference(ref)
			case "torn_journal":
				f, err := os.OpenFile(filepath.Join(req.Root, ".nemalo", "holdings.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := f.WriteString("{")
				if err := errors.Join(writeErr, f.Close()); err != nil {
					t.Fatal(err)
				}
			case "missing_body":
				if err := os.Remove(body); err != nil {
					t.Fatal(err)
				}
			case "changed_body":
				if err := os.WriteFile(body, []byte("replacement bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				cancel()
			case "units":
				req.UnitsOnly = true
			case "source":
				req.Representation = "epub-source/1"
			case "source_cursor":
				read := req
				read.Evidence = false
				read.EvidenceReference = ""
				read.Representation = "epub-source/1"
				read.MaxBytes = 20
				source, err := s.Content(ctx, read)
				if err != nil {
					t.Fatal(err)
				}
				req.Cursor = source.Continuation
			case "find":
				req.Find = "source"
			case "resource":
				req.Resource = true
				req.Representation = "epub-structure/1"
			}
			before := citationTree(t, filepath.Dir(body))
			result, err := s.Content(ctx, req)
			noEvidencePayload(t, result, err)
			if !reflect.DeepEqual(before, citationTree(t, filepath.Dir(body))) {
				t.Fatal("failed read changed files")
			}
			if name == "missing_root" {
				if _, err := os.Stat(req.Root); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing library created", err)
				}
			}
		})
	}
}

func TestEvidenceReferenceStrictCanonicalShape(t *testing.T) {
	req, _ := evidenceHolding(t, "book.epub", epubWithText("Text."))
	got, err := (Service{}).Content(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	valid := got.Evidence.Reference
	ref, err := parseEvidenceReference(valid)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(ref)
	for _, name := range []string{"prefix", "oversize", "padding", "whitespace", "duplicate", "extra", "schema", "hash", "library", "asset", "representation", "extractor", "json"} {
		t.Run(name, func(t *testing.T) {
			token := valid
			other := ref
			switch name {
			case "prefix":
				token = "unknown:" + strings.TrimPrefix(valid, evidencePrefix)
			case "oversize":
				token = evidencePrefix + strings.Repeat("a", 2048)
			case "padding":
				token = valid + "="
			case "whitespace":
				token = evidencePrefix + base64.RawURLEncoding.EncodeToString(append([]byte(" "), encoded...))
			case "duplicate":
				token = evidencePrefix + base64.RawURLEncoding.EncodeToString([]byte(`{"schema_version":1,`+string(encoded[1:])))
			case "extra":
				token = evidencePrefix + base64.RawURLEncoding.EncodeToString([]byte(`{"path":"/tmp/book",`+string(encoded[1:])))
			case "schema":
				other.SchemaVersion = 2
				token = encodeEvidenceReference(other)
			case "hash":
				other.RetrievalSHA256 = strings.Repeat("A", 64)
				token = encodeEvidenceReference(other)
			case "library":
				other.LibraryID = "library:bad"
				token = encodeEvidenceReference(other)
			case "asset":
				other.AssetID = "sha256:bad"
				token = encodeEvidenceReference(other)
			case "representation":
				other.RepresentationID = "bad"
				token = encodeEvidenceReference(other)
			case "extractor":
				other.Extractor = ""
				token = encodeEvidenceReference(other)
			case "json":
				token = evidencePrefix + base64.RawURLEncoding.EncodeToString([]byte("not JSON"))
			}
			if _, err := parseEvidenceReference(token); err == nil {
				t.Fatal("noncanonical reference accepted", name)
			}
		})
	}
	if strings.Contains(string(encoded), req.Root) || strings.Contains(string(encoded), "selection") || strings.Contains(string(encoded), "decode") {
		t.Fatal("reference carried paths/selectors/authority", string(encoded))
	}
}

func TestEvidenceReplayRefusesChangedRelatedTrackMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	service := Service{Scanner: citationNoScan{t}}
	hold := func(position int) string {
		frame := make([]byte, 417)
		copy(frame, []byte{0xff, 0xfb, 0x90, 0x40})
		data := bytes.Repeat(frame, 10+position)
		hash := sha256.Sum256(data)
		packet := t.TempDir()
		receipt, err := json.Marshal(map[string]any{"complete": true, "status": "untrusted_intake", "antivirus": "not_scanned", "transfer": map[string]any{"bytes": len(data), "sha256": hex.EncodeToString(hash[:])}, "intent": map[string]any{"request": map[string]any{"source_id": "archive:synthetic-recording", "source_file": fmt.Sprintf("part-%d.mp3", position), "recording": map[string]any{"source_format": "MP3", "position": position, "tracks": 2}}, "selection": map[string]any{}}})
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string][]byte{"content.mp3": data, "receipt.json": receipt} {
			if err := os.WriteFile(filepath.Join(packet, name), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
		held, err := service.Import(ctx, library.ImportRequest{Library: root, Source: packet, Apply: true})
		if err != nil {
			t.Fatal(err)
		}
		return held.AssetID
	}
	asset := hold(1)
	req := content.Request{SchemaVersion: 1, Catalog: root, Root: root, AssetID: asset, MaxBytes: 1000, Representation: "mpeg-audio/1", Evidence: true}
	first, err := service.Content(ctx, req)
	if err != nil || first.Evidence == nil || len(first.Evidence.Metadata.Recordings) != 1 || first.Evidence.Metadata.Recordings[0].Selection.Complete {
		t.Fatal(first, err)
	}
	hold(2)
	current, err := service.Content(ctx, req)
	if err != nil || !current.Evidence.Metadata.Recordings[0].Selection.Complete || current.Evidence.Metadata.HoldingSHA256 != first.Evidence.Metadata.HoldingSHA256 || current.Evidence.MetadataSHA256 == first.Evidence.MetadataSHA256 {
		t.Fatal("related-track fixture did not change only the metadata context", current, err)
	}
	req.EvidenceReference = first.Evidence.Reference
	failed, err := service.Content(ctx, req)
	noEvidencePayload(t, failed, err)
	if failed.Status != "stale_reference" {
		t.Fatal(failed, err)
	}
}

func TestEvidenceDigestExcludesPresentationButBindsSelectionAndPayload(t *testing.T) {
	result := content.Result{SchemaVersion: 1, Status: "complete_range", Text: "Exact quotation.", SourcePath: "private/path", CatalogID: "library:one", Limitations: []string{"prose"}}
	selection := content.ReadSelection{MaxBytes: 4096}
	first, err := retrievalDigest(result, selection)
	if err != nil {
		t.Fatal(err)
	}
	// This value hashes the frozen version 1 two-field JSON object, including
	// the original Result's mandatory empty/null fields. Adding a new result
	// field must not silently reinterpret already issued evidence references.
	if first != "6fb2cba3305179c18bd6e48fad04c908e8a5b282c967e14b377505564cb5d63d" {
		t.Fatal("version 1 retrieval encoding changed", first)
	}
	result.SourcePath, result.CatalogID, result.Limitations, result.Evidence = "moved/path", "library:display-only", []string{"revised prose"}, &content.Evidence{Reference: "not part of its own digest"}
	if got, err := retrievalDigest(result, selection); err != nil || got != first {
		t.Fatal("display prose/recursive evidence affected identity", got, err)
	}
	for _, name := range []string{"text", "continuation", "stop", "range", "bound", "explicit_zero"} {
		t.Run(name, func(t *testing.T) {
			r, q := result, selection
			switch name {
			case "text":
				r.Text += " changed"
			case "continuation":
				r.Continuation = "next range"
			case "stop":
				r.EndOfSource = true
			case "range":
				r.Locator = &content.Locator{Start: 1, End: 2}
			case "bound":
				q.MaxBytes++
			case "explicit_zero":
				n := 0
				q.Packet = &n
			}
			if got, err := retrievalDigest(r, q); err != nil || got == first {
				t.Fatal("changed selection/result kept identity", got, err)
			}
		})
	}
}
