package content

type RepresentationCapability struct {
	ID          string   `json:"id"`
	Extractor   string   `json:"extractor"`
	Operations  []string `json:"operations"`
	Description string   `json:"description"`
}
type Capabilities struct {
	SchemaVersion    int                        `json:"schema_version"`
	Default          string                     `json:"default_representation"`
	Representations  []RepresentationCapability `json:"representations"`
	Unsupported      []string                   `json:"unsupported"`
	MaxTextBytes     int                        `json:"max_text_bytes"`
	MaxResultBytes   int                        `json:"max_result_bytes"`
	ConsumptionState string                     `json:"consumption_state"`
}

func Available() Capabilities {
	return Capabilities{SchemaVersion: 1, Default: DefaultRepresentation, Representations: []RepresentationCapability{
		{ID: "epub-text/2", Extractor: TextExtractorV2, Operations: []string{"units", "read"}, Description: "Default. Plain text with omitted images, tables, mathematics, and media located as gaps; unsupported documents stay explicit per unit."},
		{ID: "epub-text/1", Extractor: Extractor, Operations: []string{"units", "read"}, Description: "Original conservative XHTML plain text. Any image, table, or media element makes its whole document unsupported."},
		{ID: "epub-structure/1", Extractor: structuredExtractor, Operations: []string{"units", "read", "resource"}, Description: "Source parts, links, language/direction, and located extraction gaps; no rendering."},
		{ID: "epub-source/1", Extractor: structuredExtractor, Operations: []string{"read", "source"}, Description: "Bounded original XHTML/XML member bytes or selected source part; untrusted markup."},
	}, Unsupported: []string{"PDF text/page/OCR extraction", "MP3/M4B recording manifests and decoded audio ranges", "table grid reconstruction", "mathematical rendering", "MCP server"}, MaxTextBytes: 65536, MaxResultBytes: 1 << 20, ConsumptionState: "none; retrieval never acknowledges reading or listening"}
}
