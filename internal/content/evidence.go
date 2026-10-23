package content

import "github.com/blisspixel/nemalo/internal/library"

// ReadSelection repeats the original bounded selection. It contains neither
// filesystem roots nor decoder permission. A reference cannot supply authority.
type ReadSelection struct {
	Representation string `json:"representation,omitempty"`
	Unit           int    `json:"unit,omitempty"`
	Offset         int    `json:"text_offset,omitempty"`
	MaxBytes       int    `json:"max_bytes"`
	Cursor         string `json:"cursor,omitempty"`
	Part           string `json:"part,omitempty"`
	Sample         int64  `json:"sample,omitempty"`
	Packet         *int   `json:"packet,omitempty"`
	PCMSample      *int64 `json:"pcm_sample,omitempty"`
	Navigation     string `json:"navigation,omitempty"`
	Label          string `json:"label,omitempty"`
}

func Selection(r Request) ReadSelection {
	return ReadSelection{Representation: r.Representation, Unit: r.Unit, Offset: r.Offset, MaxBytes: r.MaxBytes, Cursor: r.Cursor, Part: r.Part, Sample: r.Sample, Packet: r.Packet, PCMSample: r.PCMSample, Navigation: r.Navigation, Label: r.Label}
}

// Evidence describes this returned range, not a reader's interpretation or
// whole-work coverage. The enclosing Result retains all original typed facts.
type Evidence struct {
	Schema           string                  `json:"schema"`
	Kind             string                  `json:"kind"`
	Metadata         library.HoldingMetadata `json:"metadata"`
	MetadataSHA256   string                  `json:"metadata_sha256"`
	Extractor        string                  `json:"extractor"`
	Representation   string                  `json:"representation"`
	RepresentationID string                  `json:"representation_id"`
	Selection        ReadSelection           `json:"selection"`
	Quotation        string                  `json:"quotation,omitempty"`
	Locator          *Locator                `json:"locator,omitempty"`
	PCMRange         *PCMLocator             `json:"pcm_range,omitempty"`
	DeliveredSHA256  string                  `json:"delivered_sha256,omitempty"`
	DeliveredBytes   int                     `json:"delivered_bytes,omitempty"`
	RetrievalSHA256  string                  `json:"retrieval_sha256"`
	Reference        string                  `json:"reference,omitempty"`
	Coverage         EvidenceCoverage        `json:"coverage"`
	Limitations      []string                `json:"limitations"`
}

type EvidenceCoverage struct {
	Scope            string    `json:"scope"`
	ReturnedUnits    []int     `json:"returned_units"`
	UnsupportedUnits []int     `json:"unsupported_units"`
	Gaps             []Gap     `json:"gaps"`
	Extraction       *Coverage `json:"extraction,omitempty"`
	HasContinuation  bool      `json:"has_continuation"`
	EndOfUnit        bool      `json:"end_of_unit"`
	EndOfSource      bool      `json:"end_of_source"`
	RangeStatus      string    `json:"range_status,omitempty"`
}
