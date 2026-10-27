package assessment

import (
	"context"
	"io"
)

// EPUBCover is a package's declared cover image (decision 0105). Declaration
// is "cover-image" (an EPUB 3 manifest property), "meta-cover" (the EPUB 2
// cover meta), "remote" for a declared remote resource that is never fetched,
// "missing" for a declaration naming no manifest item, or "" when the package
// declares none. Member and Data are set only for a local declared item.
type EPUBCover struct {
	Package     string
	Member      string
	Media       string
	Declaration string
	Data        []byte
}

// opfMeta is an EPUB 2 package meta element.
type opfMeta struct {
	Name    string `xml:"name,attr"`
	Content string `xml:"content,attr"`
}

// ReadEPUBCover performs the same complete bounded validation as every
// content read and returns the first package's declared cover member, at
// most limit bytes. It never chooses an image by name, position or size, and
// never resolves a remote reference.
func ReadEPUBCover(ctx context.Context, r io.ReaderAt, size int64, limit uint64) (Checks, EPUBCover, error) {
	if limit > 8<<20 {
		return Checks{}, EPUBCover{}, ErrLimit
	}
	request := &memberRequest{cover: true, limit: limit}
	c, err := inspectContext(ctx, r, size, "epub", nil, request, nil)
	cover := EPUBCover{Package: request.packagePath, Member: request.name, Media: request.media, Declaration: request.declaration}
	if err != nil {
		return c, cover, err
	}
	cover.Data = request.data
	return c, cover, nil
}

// declareCover records the package's cover declaration: the cover-image
// property first, then the EPUB 2 meta naming a manifest item.
func declareCover(request *memberRequest, packagePath, coverID string, metas []opfMeta, documents map[string]EPUBDocument, local map[string]bool) {
	declaration := "cover-image"
	if coverID == "" {
		declaration = "meta-cover"
		for _, m := range metas {
			if m.Name == "cover" && m.Content != "" {
				coverID = m.Content
				break
			}
		}
	}
	if coverID == "" {
		return
	}
	request.packagePath = packagePath
	d, ok := documents[coverID]
	switch {
	case !ok:
		request.declaration = "missing"
	case !local[coverID]:
		request.declaration = "remote"
	default:
		request.declaration, request.name, request.media = declaration, d.Path, d.Media
	}
}
