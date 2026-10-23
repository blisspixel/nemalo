package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

// CSLItem is a narrow standard-field export. Nemalo's provenance and unknowns
// live in the sidecar, not arbitrary properties in CSL objects.
type CSLItem struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Language string `json:"language,omitempty"`
	ISBN     string `json:"ISBN,omitempty"`
	DOI      string `json:"DOI,omitempty"`
}

type CitationField struct {
	Field      string   `json:"field"`
	Origin     string   `json:"origin"`
	Status     string   `json:"status"`
	Candidates []string `json:"candidates"`
}

type CitationResult struct {
	SchemaVersion  int                      `json:"schema_version"`
	Schema         string                   `json:"schema"`
	Status         string                   `json:"status"`
	CSLJSON        []CSLItem                `json:"csl_json,omitempty"`
	Metadata       *library.HoldingMetadata `json:"metadata,omitempty"`
	MetadataSHA256 string                   `json:"metadata_sha256,omitempty"`
	Fields         []CitationField          `json:"fields,omitempty"`
	UnknownFields  []string                 `json:"unknown_fields,omitempty"`
	Limitations    []string                 `json:"limitations"`
}

// Citation exports only a selected committed holding's recorded declarations.
// It neither opens stored content/receipts nor probes a provider or decoder.
func (s Service) Citation(ctx context.Context, directory, asset string) (out CitationResult, err error) {
	out = CitationResult{SchemaVersion: 1, Schema: "nemalo-citation/1", Status: "unavailable", Limitations: []string{
		"Recorded declarations, not a fresh byte, registry, rights or safety check. Stored content and acquisition receipt paths are not opened.",
		"Only unambiguous recorded title, language and classified ISBN/DOI values are exported. Author, date, publisher, edition label and bibliographic type remain unknown; document is a generic export fallback.",
		"Private intake paths and transfer URLs are omitted. Recording byte identities are journal evidence, and narrator credits are not authorship.",
	}}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	defer func() {
		if err != nil {
			out.Status = "unavailable"
			out.CSLJSON, out.Metadata, out.Fields, out.UnknownFields = nil, nil, nil, nil
			out.MetadataSHA256 = ""
			switch {
			case ctx.Err() != nil:
				out.Status = "cancelled"
			case errors.Is(err, library.ErrMetadataBudget):
				out.Status = "budget_exceeded"
			}
		}
	}()
	err = library.WithHoldingMetadata(ctx, directory, asset, func(metadata library.HoldingMetadata) error {
		out.Metadata = &metadata
		var err error
		out.MetadataSHA256, err = canonicalDigest(metadata)
		if err != nil {
			return err
		}
		out.CSLJSON, out.Fields, out.UnknownFields = citationFields(metadata)
		out.Status = "declared_metadata"
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

// canonicalDigest preserves JSON numbers and recursively orders object keys.
// This is the versioned Nemalo encoding, not an external JSON signature format.
func canonicalDigest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	encoded, err = json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func citationFields(metadata library.HoldingMetadata) ([]CSLItem, []CitationField, []string) {
	item := CSLItem{ID: metadata.AssetID, Type: "document"}
	unknown := []string{"author", "issued", "publisher", "edition", "type", "URL"}
	fields := []CitationField{{Field: "type", Origin: "export_fallback", Status: "bibliographic_type_unknown", Candidates: []string{"document"}}}
	selectField := func(field, origin string, candidates []string) string {
		unique := []string{}
		seen := map[string]bool{}
		for _, value := range candidates {
			if value != "" && !seen[value] {
				seen[value] = true
				unique = append(unique, value)
			}
		}
		status, selected := "unknown", ""
		if len(unique) == 1 {
			status, selected = "recorded_declaration", unique[0]
		} else {
			unknown = append(unknown, field)
			if len(unique) > 1 {
				status = "ambiguous"
			}
		}
		fields = append(fields, CitationField{Field: field, Origin: origin, Status: status, Candidates: unique})
		return selected
	}
	item.Title = selectField("title", "holding_titles", metadata.Titles)
	item.Language = selectField("language", "holding_languages", metadata.Languages)
	identities := append([]library.Identity{}, metadata.Identities...)
	for _, source := range metadata.Sources {
		identities = append(identities, source.Identities...)
	}
	var isbn, doi []string
	for _, identity := range identities {
		if value, ok := strings.CutPrefix(identity.ID, "isbn:"); ok && identity.Role == "edition" {
			isbn = append(isbn, value)
		}
		if value, ok := strings.CutPrefix(identity.ID, "doi:"); ok && identity.Role == "publication_version" {
			doi = append(doi, value)
		}
	}
	item.ISBN = selectField("ISBN", "classified_recorded_identifiers", isbn)
	item.DOI = selectField("DOI", "classified_recorded_identifiers", doi)
	sort.Strings(unknown)
	return []CSLItem{item}, fields, unknown
}
