// Package inventory provides bounded, read-only inventory of an explicitly selected root.
package inventory

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/safeio"
)

type Limits struct {
	Entries    int
	Depth      int
	FileBytes  int64
	TotalBytes int64
	Hashes     bool
	PAR2       bool
}

func Defaults() Limits {
	return Limits{Entries: 10000, Depth: 32, FileBytes: 256 << 20, TotalBytes: 1 << 30}
}

type Entry struct {
	Path    string    `json:"path"`
	Kind    string    `json:"kind"`
	Bytes   int64     `json:"bytes,omitempty"`
	SHA256  string    `json:"sha256,omitempty"`
	Finding string    `json:"finding,omitempty"`
	PAR2    *PAR2File `json:"par2_metadata,omitempty"`
}

type Report struct {
	Root      string      `json:"root"`
	Complete  bool        `json:"complete"`
	Security  string      `json:"security_status"`
	BytesRead int64       `json:"bytes_read"`
	Entries   []Entry     `json:"entries"`
	PAR2      *PAR2Report `json:"par2_metadata,omitempty"`
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
	var recovery *par2Run
	if limits.PAR2 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		recovery = newPAR2Run()
		r.PAR2 = recovery.report
	}
	seen, incomplete, traversalIncomplete := 0, false, false
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
			traversalIncomplete = true
			return nil
		}
		if d.IsDir() {
			if strings.Count(name, "/")+1 >= limits.Depth {
				r.Entries = append(r.Entries, Entry{Path: name, Kind: "directory", Finding: "depth_limit"})
				incomplete = true
				traversalIncomplete = true
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
			if limits.Hashes || limits.PAR2 {
				if !limits.PAR2 && (info.Size() > limits.FileBytes || info.Size() > limits.TotalBytes-r.BytesRead) {
					e.Finding = "byte_limit"
					incomplete = true
				} else {
					var observations []par2Observation
					probed := 0
					if recovery != nil {
						probed = recovery.report.FilesProbed
					}
					e.SHA256, e.PAR2, observations, err = readRegular(ctx, root, name, info, limits, &r, recovery)
					if recovery != nil {
						metadataComplete := recovery.report.FilesProbed > probed
						if e.PAR2 != nil && e.PAR2.State != "source_changed" {
							recovery.add(name, e.PAR2, observations)
						}
						if e.PAR2 != nil {
							metadataComplete = e.PAR2.completed()
						}
						recovery.report.Complete = recovery.report.Complete && metadataComplete
					}
					if err != nil {
						e.Finding = err.Error()
						incomplete = true
					}
				}
			}
		}
		r.Entries = append(r.Entries, e)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	})
	r.Complete = err == nil && !incomplete
	if recovery != nil {
		recovery.report.Complete = recovery.report.Complete && err == nil && !traversalIncomplete
		recovery.finish()
	}
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
