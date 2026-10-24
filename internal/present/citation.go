package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

func Citation(r app.CitationResult) string {
	var b strings.Builder
	field(&b, "Citation", words(r.Status))
	field(&b, "Metadata SHA-256", Line(r.MetadataSHA256))
	if r.Metadata != nil {
		field(&b, "Asset", Line(r.Metadata.AssetID))
		field(&b, "Library", Line(r.Metadata.LibraryID))
		field(&b, "Bytes", words(r.Metadata.ByteStatus))
	}
	if len(r.CSLJSON) > 0 {
		if data, err := textsafe.MarshalJSON(r.CSLJSON, true); err == nil {
			b.WriteString("CSL-JSON item array:\n")
			b.Write(data)
			b.WriteByte('\n')
		}
	}
	if len(r.UnknownFields) > 0 {
		field(&b, "Unknown fields", Line(strings.Join(r.UnknownFields, ", ")))
	}
	for _, f := range r.Fields {
		if f.Status == "ambiguous" {
			fmt.Fprintf(&b, "Ambiguous %s: %s\n", Line(f.Field), Line(strings.Join(f.Candidates, " | ")))
		}
	}
	for _, limit := range r.Limitations {
		fmt.Fprintf(&b, "Limit: %s\n", Line(limit))
	}
	if r.Metadata != nil {
		if data, err := textsafe.MarshalJSON(r, true); err == nil {
			b.WriteString("Portable citation with provenance and uncertainty (JSON):\n")
			b.Write(data)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
