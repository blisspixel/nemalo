package content

import (
	"github.com/blisspixel/nemalo/internal/audio"
	"github.com/blisspixel/nemalo/internal/pdftext"
)

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
		{ID: "epub-text/2", Extractor: TextExtractorV2, Operations: []string{"units", "read", "locate"}, Description: "Default. Plain text with omitted images, tables, mathematics, and media located as gaps; unsupported documents stay explicit per unit."},
		{ID: "epub-text/1", Extractor: Extractor, Operations: []string{"units", "read"}, Description: "Original conservative XHTML plain text. Any image, table, or media element makes its whole document unsupported."},
		{ID: "epub-structure/1", Extractor: structuredExtractor, Operations: []string{"units", "read", "resource"}, Description: "Source parts, links, language/direction, and located extraction gaps; no rendering."},
		{ID: "epub-source/1", Extractor: structuredExtractor, Operations: []string{"read", "source"}, Description: "Bounded original XHTML/XML member bytes or selected source part; untrusted markup."},
		{ID: "epub-nav/1", Extractor: NavExtractor, Operations: []string{"units", "read"}, Description: "Source-declared EPUB 3 nav and EPUB 2 NCX hierarchy. A read follows one declared entry until its declared end, which is not a spine boundary or a truncated result."},
		{ID: "epub-page-list/1", Extractor: PageListExtractor, Operations: []string{"units", "read"}, Description: "Source-declared EPUB 3 page-list markers, such as iv or 42, with exact targets in epub-text/2 coordinates and the stated pagination source. A read starts at one marker, by reference or exact label. Not screen pages or a print-edition match."},
		{ID: "pdf-text/1", Extractor: pdftext.Extractor, Operations: []string{"units", "read", "locate"}, Description: "One unit per physical PDF page with its printed page label; text in content-stream order with images, unmapped font codes, and pages without a text layer located as gaps; no OCR or column reconstruction."},
		{ID: "mpeg-audio/1", Extractor: audio.Extractor, Operations: []string{"units", "read"}, Description: "MP3 frame index with probed versus declared timing, ID3 chapters, and frame-aligned byte ranges with decoder preroll; never decoded."},
		{ID: "mp4-audio/1", Extractor: audio.MP4Extractor, Operations: []string{"units"}, Description: "M4B or M4A sample tables with every sample located in the file, encoded versus declared timing, and QuickTime or Nero chapters; units only, never decoded."},
		{ID: MP4PacketRepresentation, Extractor: audio.MP4PacketExtractor, Operations: []string{"units", "read"}, Description: "Exact original MP4 audio packets and bounded untrusted sample-entry bytes, with individual source spans and media decode ticks. Not a playable MP4 container or decoded samples. Each request may prepare 2 GiB private scratch and read 4 GiB of original source; 64 KiB/128 packet output, 16 KiB initialization, five-minute ceiling."},
	}, Unsupported: []string{"PDF OCR, column reading order, and rendering", "decoded audio, edited MP4 presentation/sample timing, and standalone MP4 range containers", "table grid reconstruction", "mathematical rendering"}, MaxTextBytes: 65536, MaxResultBytes: 1 << 20, ConsumptionState: "none; retrieval never acknowledges reading or listening"}
}
