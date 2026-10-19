// Package staging extracts archive members into a private staging directory
// for assessment and import. It never writes beside the source, follows links,
// or trusts member names as paths, and it accounts for every member.
package staging

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// Limits bound one archive. Declared sizes are checked before extraction and
// actual bytes are capped while copying, so a lying header cannot exceed them.
type Limits struct {
	Members     int
	MemberBytes int64
	TotalBytes  int64
	Ratio       uint64
}

// Defaults bound one archive. EPUB, PDF, MP3, and MP4 audio are compressed, so a
// genuine member expands only a few times in the ZIP; 100:1 marks a member as
// suspicious long before the byte caps, which bind what is actually written.
func Defaults() Limits {
	return Limits{Members: 10000, MemberBytes: 256 << 20, TotalBytes: 1 << 30, Ratio: 100}
}

// Member records what happened to one archive entry. Staged is the file name
// in the staging directory and is set only for extracted members.
type Member struct {
	Name        string `json:"name"`
	Bytes       int64  `json:"declared_bytes"`
	Disposition string `json:"disposition"`
	Detail      string `json:"detail,omitempty"`
	Staged      string `json:"-"`
}

// Extraction lists every member. Complete is false when any eligible member
// could not be extracted; unsupported members are preserved in the archive and
// do not make it incomplete.
type Extraction struct {
	Members  []Member `json:"members"`
	Complete bool     `json:"complete"`
	// Whole reports, for an archive read through 7-Zip, that its archive
	// block names no comment, warning, leading offset, or trailing bytes, and
	// a physical size equal to the bytes read, so the members are all it
	// holds. Cleanup moves such an archive only when Whole.
	Whole bool `json:"whole,omitempty"`
}

var nestedArchive = map[string]bool{".zip": true, ".rar": true, ".7z": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".zst": true, ".cbz": true}

// ExtractZip reads the ZIP file name in src and writes each member whose name
// passes supported into dst under a fresh, index-prefixed flat name. Directories,
// links, encrypted or unknown-method members, nested archives, unsafe names,
// oversized or suspiciously compressed members, and unsupported formats are
// recorded and left in the archive.
func ExtractZip(ctx context.Context, src *os.Root, name string, dst *os.Root, supported func(string) bool, limits Limits) (Extraction, error) {
	x := Extraction{Members: []Member{}}
	info, err := src.Lstat(name)
	if err != nil {
		return x, err
	}
	f, err := safeio.OpenRegular(src, name, info, os.O_RDONLY)
	if err != nil {
		return x, err
	}
	defer f.Close()
	z, err := zip.NewReader(f, info.Size())
	if err != nil {
		return x, fmt.Errorf("not a readable ZIP archive: %w", err)
	}
	if len(z.File) > limits.Members {
		return x, fmt.Errorf("archive has %d members; the limit is %d", len(z.File), limits.Members)
	}
	var total int64
	x.Complete = true
	// A member name is its provenance, so a repeated name extracts only once.
	seen := map[string]bool{}
	for i, entry := range z.File {
		if err := ctx.Err(); err != nil {
			return x, err
		}
		m := Member{Name: displayName(entry.Name), Bytes: int64(min(entry.UncompressedSize64, 1<<62))}
		m.Disposition, m.Detail = classify(entry, supported, limits)
		if m.Disposition == "eligible" && seen[entry.Name] {
			m.Disposition, m.Detail = "duplicate_name", "another member has this exact name; preserved in the archive"
		}
		if m.Disposition == "eligible" {
			seen[entry.Name] = true
			if total+m.Bytes > limits.TotalBytes {
				m.Disposition, m.Detail = "too_large", "archive total exceeds the extraction budget"
				x.Complete = false
			} else {
				m.Staged = fmt.Sprintf("%05d%s", i, strings.ToLower(path.Ext(entry.Name)))
				if err := safeio.EnsureSpaceIn(dst, m.Bytes); err != nil {
					// Too little space: the member stays in the archive.
					m.Disposition, m.Detail, m.Staged = "failed", err.Error(), ""
					x.Complete = false
				} else if err := extract(ctx, entry, dst, m.Staged, limits.MemberBytes); err != nil {
					m.Disposition, m.Detail, m.Staged = "failed", err.Error(), ""
					_ = dst.Remove(fmt.Sprintf("%05d%s", i, strings.ToLower(path.Ext(entry.Name))))
					x.Complete = false
				} else {
					m.Disposition = "extracted"
					total += m.Bytes
				}
			}
		}
		x.Members = append(x.Members, m)
	}
	return x, nil
}

// classify decides a member's disposition before any byte is written.
func classify(entry *zip.File, supported func(string) bool, limits Limits) (string, string) {
	name := entry.Name
	switch {
	case strings.HasSuffix(name, "/") || entry.Mode().IsDir():
		return "directory", ""
	case len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || !fs.ValidPath(name):
		return "unsafe_name", "absolute, parent, drive, or malformed member path; not extracted"
	case entry.Mode()&fs.ModeSymlink != 0:
		return "link", "not followed"
	case entry.Mode()&fs.ModeType != 0:
		return "special", "not a regular file"
	case entry.Flags&0x1 != 0:
		return "encrypted", "encrypted members are not extracted"
	case entry.Method != zip.Store && entry.Method != zip.Deflate:
		return "unsupported_method", fmt.Sprintf("compression method %d", entry.Method)
	case nestedArchive[strings.ToLower(path.Ext(name))]:
		return "nested_archive", "preserved in the archive; not extracted"
	case !supported(name):
		return "unsupported_format", "preserved in the archive"
	case int64(min(entry.UncompressedSize64, 1<<62)) > limits.MemberBytes:
		return "too_large", "member exceeds the per-file budget"
	case entry.UncompressedSize64 == 0:
		return "empty", ""
	case entry.CompressedSize64 == 0 || entry.UncompressedSize64/entry.CompressedSize64 > limits.Ratio:
		return "suspicious_ratio", "declared expansion ratio is beyond the budget"
	}
	return "eligible", ""
}

// extract copies one member, capped at its declared size plus one byte so a
// lying header is detected, and checks its CRC.
func extract(ctx context.Context, entry *zip.File, dst *os.Root, staged string, limit int64) error {
	rc, err := entry.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := dst.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	declared := int64(entry.UncompressedSize64)
	// archive/zip verifies the CRC only when the recorded value is non-zero, so
	// the CRC is computed here for every member.
	sum := crc32.NewIEEE()
	n, copyErr := io.Copy(io.MultiWriter(out, sum), io.LimitReader(contextReader{ctx, rc}, min(declared, limit)+1))
	closeErr := out.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if n != declared {
		return fmt.Errorf("member holds %d bytes, not the declared %d", n, declared)
	}
	if sum.Sum32() != entry.CRC32 {
		return fmt.Errorf("member CRC %08x differs from the recorded %08x", sum.Sum32(), entry.CRC32)
	}
	return nil
}

// displayName keeps member names printable and bounded for reports.
func displayName(name string) string {
	name = strings.ToValidUTF8(name, "\uFFFD")
	if len(name) > 1024 {
		end := 1024
		for end > 0 && !utf8.RuneStart(name[end]) {
			end--
		}
		name = name[:end]
	}
	return name
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
