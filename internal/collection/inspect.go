package collection

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

type Checks struct {
	Signature     string   `json:"signature"`
	ZIPCRC        string   `json:"zip_crc"`
	EPUBPackage   string   `json:"epub_package"`
	Conformance   string   `json:"conformance"`
	ContentReview string   `json:"content_review"`
	Members       int      `json:"members,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
}

// Inspect performs bounded container checks without rendering or executing content.
// PDF/MP3 signatures are candidates, not parser, decoder, or malware validation.
func Inspect(r io.ReaderAt, size int64, format string) (Checks, error) {
	c := Checks{Signature: "not_checked", ZIPCRC: "not_applicable", EPUBPackage: "not_applicable", Conformance: "not_checked", ContentReview: "not_checked"}
	if size <= 0 || size > 256<<20 {
		return c, errors.New("invalid asset size")
	}
	header := make([]byte, min(size, int64(8)))
	if _, err := r.ReadAt(header, 0); err != nil {
		return c, err
	}
	switch format {
	case "pdf":
		if !bytes.HasPrefix(header, []byte("%PDF-")) {
			return c, errors.New("PDF header missing")
		}
		tail := make([]byte, min(size, int64(2048)))
		if _, err := r.ReadAt(tail, size-int64(len(tail))); err != nil {
			return c, err
		}
		if !bytes.Contains(tail, []byte("%%EOF")) {
			return c, errors.New("PDF end marker missing")
		}
		c.Signature = "pdf_header_and_end_marker"
	case "mp3":
		if !bytes.HasPrefix(header, []byte("ID3")) && !(len(header) >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0) {
			return c, errors.New("MP3 candidate signature missing")
		}
		c.Signature = "mp3_candidate_only"
	case "epub":
		if !bytes.HasPrefix(header, []byte("PK\x03\x04")) {
			return c, errors.New("ZIP signature missing")
		}
		c.Signature = "zip"
		if err := inspectEPUB(r, size, &c); err != nil {
			return c, err
		}
	default:
		return c, errors.New("unsupported assessment format")
	}
	return c, nil
}

func inspectEPUB(r io.ReaderAt, size int64, c *Checks) error {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	if len(z.File) == 0 || len(z.File) > 10000 {
		return errors.New("EPUB member count outside limits")
	}
	files := map[string]*zip.File{}
	var expanded uint64
	for _, f := range z.File {
		name := strings.TrimSuffix(f.Name, "/")
		if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:\x00") || f.Mode()&fs.ModeSymlink != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) || files[f.Name] != nil {
			return fmt.Errorf("unsafe or duplicate EPUB member %q", f.Name)
		}
		files[f.Name] = f
		if f.UncompressedSize64 > 32<<20 || expanded > 128<<20-f.UncompressedSize64 {
			return errors.New("EPUB expanded size exceeds limit")
		}
		expanded += f.UncompressedSize64
		body, err := f.Open()
		if err != nil {
			return err
		}
		n, err := io.Copy(io.Discard, io.LimitReader(body, int64(f.UncompressedSize64)+1))
		closeErr := body.Close()
		if err != nil || closeErr != nil || n != int64(f.UncompressedSize64) {
			return fmt.Errorf("EPUB member failed CRC/length checks: %q", f.Name)
		}
	}
	c.Members, c.ZIPCRC = len(files), "passed"
	mt, err := member(files, "mimetype", 128)
	if err != nil || string(mt) != "application/epub+zip" || z.File[0].Name != "mimetype" || z.File[0].Method != zip.Store {
		return errors.New("invalid EPUB mimetype placement, compression, or value")
	}
	container, err := member(files, "META-INF/container.xml", 1<<20)
	if err != nil {
		return err
	}
	var doc struct {
		XMLName xml.Name `xml:"urn:oasis:names:tc:opendocument:xmlns:container container"`
		Roots   []struct {
			Path  string `xml:"full-path,attr"`
			Media string `xml:"media-type,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(container, &doc); err != nil || len(doc.Roots) == 0 || len(doc.Roots) > 8 {
		return errors.New("invalid EPUB container document")
	}
	for _, entry := range doc.Roots {
		if !fs.ValidPath(entry.Path) || strings.ContainsAny(entry.Path, "\\:") || entry.Media != "application/oebps-package+xml" {
			return errors.New("invalid EPUB package path or media type")
		}
		data, err := member(files, entry.Path, 2<<20)
		if err != nil {
			return err
		}
		var pkg struct {
			XMLName xml.Name `xml:"http://www.idpf.org/2007/opf package"`
			Items   []struct {
				ID   string `xml:"id,attr"`
				Href string `xml:"href,attr"`
			} `xml:"manifest>item"`
			Spine []struct {
				ID string `xml:"idref,attr"`
			} `xml:"spine>itemref"`
		}
		if err := xml.Unmarshal(data, &pkg); err != nil || len(pkg.Items) == 0 || len(pkg.Spine) == 0 {
			return errors.New("invalid EPUB package manifest or spine")
		}
		ids := map[string]bool{}
		for _, item := range pkg.Items {
			if item.ID == "" || ids[item.ID] || item.Href == "" {
				return errors.New("invalid EPUB manifest identity")
			}
			ids[item.ID] = true
			// Remote resource declarations need content policy review; never fetch them.
			if strings.Contains(item.Href, ":") || strings.HasPrefix(item.Href, "//") {
				c.Warnings = append(c.Warnings, "remote manifest resource declared")
				continue
			}
			name := path.Join(path.Dir(entry.Path), strings.Split(item.Href, "#")[0])
			if !fs.ValidPath(name) || files[name] == nil {
				return fmt.Errorf("EPUB manifest references missing member %q", name)
			}
		}
		for _, spine := range pkg.Spine {
			if !ids[spine.ID] {
				return errors.New("EPUB spine references missing manifest item")
			}
		}
	}
	c.EPUBPackage = "container_manifest_spine_checked"
	return nil
}

func member(files map[string]*zip.File, name string, limit uint64) ([]byte, error) {
	f := files[name]
	if f == nil || f.UncompressedSize64 > limit {
		return nil, fmt.Errorf("EPUB member missing or too large: %q", name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, int64(limit)+1))
}
