package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

const evidencePrefix = "nemaloevidence1:"

var errEvidenceStale = errors.New("evidence reference does not match the selected metadata, content or bounded range")

type evidenceReference struct {
	SchemaVersion    int    `json:"schema_version"`
	AssetID          string `json:"asset_id"`
	LibraryID        string `json:"library_id"`
	HoldingSHA256    string `json:"holding_sha256"`
	MetadataSHA256   string `json:"metadata_sha256"`
	Extractor        string `json:"extractor"`
	RepresentationID string `json:"representation_id"`
	RetrievalSHA256  string `json:"retrieval_sha256"`
}

func lowerHash(value string, size int) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == size && hex.EncodeToString(b) == value
}

func parseEvidenceReference(token string) (evidenceReference, error) {
	var ref evidenceReference
	if len(token) > 2048 || !strings.HasPrefix(token, evidencePrefix) {
		return ref, errors.New("invalid evidence reference prefix or length")
	}
	raw := strings.TrimPrefix(token, evidencePrefix)
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return ref, errors.New("invalid evidence reference encoding")
	}
	if err := json.Unmarshal(b, &ref); err != nil {
		return ref, errors.New("invalid evidence reference JSON")
	}
	canonical, err := json.Marshal(ref)
	if err != nil || !bytes.Equal(b, canonical) || base64.RawURLEncoding.EncodeToString(b) != raw || ref.SchemaVersion != 1 || !strings.HasPrefix(ref.AssetID, "sha256:") || !lowerHash(strings.TrimPrefix(ref.AssetID, "sha256:"), 32) || !strings.HasPrefix(ref.LibraryID, "library:") || !lowerHash(strings.TrimPrefix(ref.LibraryID, "library:"), 16) || !lowerHash(ref.HoldingSHA256, 32) || !lowerHash(ref.MetadataSHA256, 32) || !strings.HasPrefix(ref.RepresentationID, "sha256:") || !lowerHash(strings.TrimPrefix(ref.RepresentationID, "sha256:"), 32) || !lowerHash(ref.RetrievalSHA256, 32) || ref.Extractor == "" || len(ref.Extractor) > 80 {
		return ref, errors.New("noncanonical or unsupported evidence reference fields")
	}
	return ref, nil
}

func encodeEvidenceReference(ref evidenceReference) string {
	b, _ := json.Marshal(ref)
	return evidencePrefix + base64.RawURLEncoding.EncodeToString(b)
}

func retrievalDigest(result content.Result, selection content.ReadSelection) (string, error) {
	result.Evidence, result.Limitations, result.SourcePath, result.CatalogID = nil, nil, "", ""
	return canonicalDigest(struct {
		Result    content.Result        `json:"result"`
		Selection content.ReadSelection `json:"selection"`
	}{result, selection})
}

func (s Service) contentEvidence(ctx context.Context, req content.Request) (out content.Result, err error) {
	out = content.Result{SchemaVersion: 1, Status: "invalid_request", AssetID: req.AssetID, Limitations: []string{"Evidence requires a managed holding and an explicit bounded read. No interpretation or reading history is stored."}}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	defer func() {
		if err != nil {
			status := out.Status
			switch {
			case ctx.Err() != nil:
				status = "cancelled"
			case errors.Is(err, errEvidenceStale):
				status = "stale_reference"
			case errors.Is(err, library.ErrMetadataBudget):
				status = "budget_exceeded"
			case errors.Is(err, library.ErrStateReview):
				status = "needs_review"
			case status == "complete_range" || status == "partial_range" || out.Evidence != nil:
				status = "unavailable"
			}
			// Rebuild, rather than enumerating payload fields: a future successful
			// result field must not accidentally survive an evidence failure.
			out = content.Result{SchemaVersion: 1, Status: status, AssetID: req.AssetID, Limitations: []string{"Evidence failed; no quotation, resource, continuation or reference is returned."}}
		}
	}()
	if err = req.Validate(); err != nil {
		return out, err
	}
	var expected evidenceReference
	if req.EvidenceReference != "" {
		expected, err = parseEvidenceReference(req.EvidenceReference)
		if err != nil {
			return out, err
		}
		if expected.AssetID != req.AssetID {
			return out, errEvidenceStale
		}
	}
	catalog, err := filepath.Abs(req.Catalog)
	if err != nil {
		return out, err
	}
	root, err := filepath.Abs(req.Root)
	if err != nil {
		return out, err
	}
	if catalog != root {
		return out, errors.New("evidence requires the same managed library as catalog and explicit root")
	}
	selection := content.Selection(req)
	out.Status = "unavailable"
	err = library.WithHoldingMetadata(ctx, catalog, req.AssetID, func(metadata library.HoldingMetadata) error {
		metadataHash, err := canonicalDigest(metadata)
		if err != nil {
			return err
		}
		if req.EvidenceReference != "" && (expected.LibraryID != metadata.LibraryID || expected.HoldingSHA256 != metadata.HoldingSHA256 || expected.MetadataSHA256 != metadataHash) {
			return errEvidenceStale
		}
		read := req
		read.Evidence, read.EvidenceReference = false, ""
		out, err = content.GetWithDecoder(ctx, read, s.PCMDecoder)
		if err != nil {
			return err
		}
		if out.CatalogID != metadata.LibraryID {
			return errEvidenceStale
		}
		digest, err := retrievalDigest(out, selection)
		if err != nil {
			return err
		}
		ref := evidenceReference{SchemaVersion: 1, AssetID: req.AssetID, LibraryID: metadata.LibraryID, HoldingSHA256: metadata.HoldingSHA256, MetadataSHA256: metadataHash, Extractor: out.Extractor, RepresentationID: out.RepresentationID, RetrievalSHA256: digest}
		if req.EvidenceReference != "" && ref != expected {
			return errEvidenceStale
		}
		evidence := &content.Evidence{Schema: "nemalo-evidence/1", Kind: "unsupported", Metadata: metadata, MetadataSHA256: metadataHash, Extractor: out.Extractor, Representation: out.Representation, RepresentationID: out.RepresentationID, Selection: selection, RetrievalSHA256: digest, Coverage: content.EvidenceCoverage{Scope: "single_returned_range", ReturnedUnits: []int{}, UnsupportedUnits: append([]int{}, out.UnsupportedUnits...), Gaps: append([]content.Gap{}, out.Gaps...), Extraction: out.Coverage, HasContinuation: out.Continuation != "", EndOfUnit: out.EndOfUnit, EndOfSource: out.EndOfSource, RangeStatus: out.RangeStatus}, Limitations: []string{
			"Source retrieval only. Caller interpretations are separate and are neither accepted nor stored. Audio bytes are not a transcript.",
			"Returned units identify this range only. Other listed units are inventory, not proof they were read. End markers do not establish whole-work coverage or understanding.",
			"Metadata is recorded declaration evidence; retrieved bytes are revalidated independently. Rights, safety and semantic completeness remain unknown.",
			"A reference compares content and selection; it grants no filesystem, network or decoder permission. Repeat the selection explicitly; a continuation selects the next range.",
		}}
		if out.Locator != nil {
			evidence.Locator = out.Locator
			evidence.Coverage.ReturnedUnits = append(evidence.Coverage.ReturnedUnits, out.Locator.Unit)
			evidence.Kind, evidence.Quotation = "source_text", out.Text
		}
		if out.Resource != nil {
			hash := sha256.Sum256(out.Resource.Data)
			evidence.DeliveredSHA256, evidence.DeliveredBytes = hex.EncodeToString(hash[:]), len(out.Resource.Data)
			evidence.Kind = "original_audio_bytes"
		}
		if out.MP4PCM != nil && out.MP4PCM.Range != nil {
			evidence.Kind, evidence.PCMRange = "decoded_audio_samples", out.MP4PCM.Range
			evidence.Coverage.ReturnedUnits = []int{out.MP4PCM.Range.Unit}
		}
		if evidence.Kind != "unsupported" {
			evidence.Reference = encodeEvidenceReference(ref)
			if _, err := parseEvidenceReference(evidence.Reference); err != nil {
				return err
			}
		}
		out.Evidence = evidence
		encoded, err := textsafe.MarshalJSON(out, false)
		if err != nil {
			return err
		}
		if len(encoded) > 1<<20 {
			return library.ErrMetadataBudget
		}
		return ctx.Err()
	})
	return out, err
}
