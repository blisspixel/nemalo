// Package inventory provides bounded, read-only inventory of an explicitly selected root.
package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/blisspixel/nemalo/internal/safeio"
)

type Limits struct {
	Entries    int
	Depth      int
	FileBytes  int64
	TotalBytes int64
	Hashes     bool
}

func Defaults() Limits {
	return Limits{Entries: 10000, Depth: 32, FileBytes: 256 << 20, TotalBytes: 1 << 30}
}

type Entry struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Bytes   int64  `json:"bytes,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	Finding string `json:"finding,omitempty"`
}

type Report struct {
	Root      string  `json:"root"`
	Complete  bool    `json:"complete"`
	Security  string  `json:"security_status"`
	BytesRead int64   `json:"bytes_read"`
	Entries   []Entry `json:"entries"`
}

// Inspect never extracts archives, scans for malware, publishes, or deletes content.
func Inspect(ctx context.Context, path string, limits Limits) (Report, error) {
	r := Report{Security: "not_scanned", Entries: []Entry{}}
	if limits.Entries < 1 || limits.Entries > 100000 || limits.Depth < 1 || limits.Depth > 256 || limits.FileBytes < 1 || limits.TotalBytes < 1 {
		return r, errors.New("invalid inventory limits")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return r, err
	}
	r.Root = abs
	root, err := safeio.OpenRoot(abs)
	if err != nil {
		return r, err
	}
	defer root.Close()
	return InspectRoot(ctx, root, abs, limits)
}

// InspectRoot retains the caller's already bound root through later assessment.
// label is reporting text only; it is never reopened or used as authority.
func InspectRoot(ctx context.Context, root *os.Root, label string, limits Limits) (Report, error) {
	r := Report{Root: label, Security: "not_scanned", Entries: []Entry{}}
	if limits.Entries < 1 || limits.Entries > 100000 || limits.Depth < 1 || limits.Depth > 256 || limits.FileBytes < 1 || limits.TotalBytes < 1 {
		return r, errors.New("invalid inventory limits")
	}
	seen, incomplete := 0, false
	err := fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == "." {
			return walkErr
		}
		seen++
		if seen > limits.Entries {
			return errors.New("inventory entry limit reached")
		}
		if walkErr != nil {
			r.Entries = append(r.Entries, Entry{Path: name, Kind: "unreadable", Finding: walkErr.Error()})
			incomplete = true
			return nil
		}
		if d.IsDir() {
			if strings.Count(name, "/")+1 >= limits.Depth {
				r.Entries = append(r.Entries, Entry{Path: name, Kind: "directory", Finding: "depth_limit"})
				incomplete = true
				return fs.SkipDir
			}
			return nil
		}
		e := Entry{Path: name}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			e.Kind = "link"
			e.Finding = "not_followed"
		} else if !info.Mode().IsRegular() {
			e.Kind = "special"
			e.Finding = "not_opened"
		} else {
			e.Kind, e.Bytes = kind(name), info.Size()
			if limits.Hashes {
				if info.Size() > limits.FileBytes || info.Size() > limits.TotalBytes-r.BytesRead {
					e.Finding = "byte_limit"
					incomplete = true
				} else {
					e.SHA256, err = hash(ctx, root, name, info, limits.TotalBytes-r.BytesRead, &r.BytesRead)
					if err != nil {
						e.Finding = err.Error()
						incomplete = true
					}
				}
			}
		}
		r.Entries = append(r.Entries, e)
		return nil
	})
	r.Complete = err == nil && !incomplete
	if err == nil && incomplete {
		err = errors.New("inventory incomplete; see entry findings")
	}
	return r, err
}

func kind(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".epub":
		return "ebook_candidate"
	case ".pdf":
		return "pdf_candidate"
	case ".mp3", ".m4b", ".m4a", ".flac", ".ogg":
		return "audio_candidate"
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".bz2", ".xz":
		return "archive_candidate"
	case ".par2":
		return "recovery_data"
	default:
		return "other"
	}
}

func hash(ctx context.Context, root *os.Root, name string, before fs.FileInfo, budget int64, read *int64) (string, error) {
	f, err := safeio.OpenRegular(root, name, before, os.O_RDONLY)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return "", errors.New("file changed before hashing")
	}
	h := sha256.New()
	buf := make([]byte, 32<<10)
	var size int64
	for size < before.Size() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := f.Read(buf[:min(int64(len(buf)), budget-size, before.Size()-size)])
		size += int64(n)
		*read += int64(n)
		if size > before.Size() || size > budget {
			return "", errors.New("file grew or exceeded read budget")
		}
		_, _ = h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	after, err := f.Stat()
	if err != nil {
		return "", err
	}
	if size != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return "", fmt.Errorf("file changed while hashing %q", name)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
