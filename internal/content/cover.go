package content

import (
	"context"
	"errors"
	"io"

	"github.com/blisspixel/nemalo/internal/assessment"
)

// maxCoverBytes bounds the declared cover member (decision 0105).
const maxCoverBytes = 8 << 20

// Cover is an EPUB's declared cover member as stored bytes. Content never
// decodes, renders or scans them; the terminal decodes under its own limits.
// Status is "declared", "none", "remote" (declared, never fetched),
// "missing" (the declaration names no manifest item), or a content status
// such as "missing_asset", "changed_source", "budget_exceeded" or
// "unsupported_extraction" when err is set.
type Cover struct {
	AssetID     string
	Status      string
	Member      string
	Media       string
	Declaration string
	Data        []byte
}

// GetCover validates the whole EPUB as a content read does, through the same
// opened-identity and whole-file hash checks, and returns only its declared
// cover member. It takes no lock, writes nothing and makes no request.
func GetCover(ctx context.Context, catalog, root, assetID string) (Cover, error) {
	cover := Cover{AssetID: assetID, Status: "invalid_request"}
	req := Request{SchemaVersion: 1, Catalog: catalog, Root: root, AssetID: assetID, MaxBytes: 4096}
	if err := req.Validate(); err != nil {
		return cover, err
	}
	ctx, cancel := context.WithTimeout(ctx, requestDeadline)
	defer cancel()
	var result Result
	var found assessment.EPUBCover
	err := resolveFile(ctx, req, &result, func(r io.ReaderAt, size int64) error {
		var err error
		_, found, err = assessment.ReadEPUBCover(ctx, r, size, maxCoverBytes)
		return err
	})
	if err != nil {
		cover.Status = result.Status
		switch {
		case ctx.Err() != nil:
			cover.Status = "cancelled"
		case errors.Is(err, assessment.ErrLimit):
			cover.Status = "budget_exceeded"
		case cover.Status == "" || cover.Status == "unavailable" && result.SourcePath != "":
			cover.Status = "unsupported_extraction"
		}
		return cover, err
	}
	cover.Member, cover.Media, cover.Declaration = found.Member, found.Media, found.Declaration
	switch found.Declaration {
	case "":
		cover.Status = "none"
	case "remote", "missing":
		cover.Status = found.Declaration
	default:
		cover.Status, cover.Data = "declared", found.Data
	}
	return cover, nil
}
