package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// ExportResult describes one verified copy of a managed holding.
type ExportResult struct {
	LibraryID   string   `json:"library_id"`
	AssetID     string   `json:"asset_id"`
	Format      string   `json:"format"`
	Title       string   `json:"title,omitempty"`
	Path        string   `json:"path"`
	Bytes       int64    `json:"bytes"`
	SHA256      string   `json:"sha256"`
	ReadOnly    bool     `json:"read_only"`
	Reused      bool     `json:"reused"`
	Launched    string   `json:"launched,omitempty"`
	Limitations []string `json:"limitations"`
}

// ExportRequest copies one holding into an explicit existing directory. Reuse
// is for handoff copies: an identical earlier copy is kept, and one a reader
// changed is left in place beside a fresh copy. Without Reuse an existing
// name is never overwritten.
type ExportRequest struct {
	Library   string
	AssetID   string
	Directory string
	ReadOnly  bool
	Reuse     bool
}

// Export never hands out the stored file itself. Readers can write into the
// files they open (calibre stores highlights inside EPUBs), and a stored file
// is identified by its bytes. The copy is hashed while written and must equal
// the holding before it is reported.
func Export(ctx context.Context, request ExportRequest) (ExportResult, error) {
	r := ExportResult{AssetID: request.AssetID, ReadOnly: request.ReadOnly, Limitations: []string{
		"This is a copy. Changes a reader or player makes to it do not reach the library, and library audit does not watch it.",
		"Opening a file in another application is not evidence of reading or listening.",
	}}
	if !validAssetID(request.AssetID) || request.Library == "" || request.Directory == "" {
		return r, errors.New("export requires a library directory, an exact sha256: asset ID, and a destination directory")
	}
	root, err := filepath.Abs(request.Library)
	if err != nil {
		return r, err
	}
	h, libraryID, err := currentHolding(ctx, root, request.AssetID)
	if err != nil {
		return r, err
	}
	r.LibraryID, r.Format, r.SHA256, r.Bytes = libraryID, h.Format, h.SHA256, h.Bytes
	if len(h.Titles) > 0 {
		r.Title = h.Titles[0]
	}
	// Reported paths use the requested directory: on Unix a root's Name() is a
	// descriptor-bridge path that is not reusable after this call.
	shown, err := filepath.Abs(request.Directory)
	if err != nil {
		return r, err
	}
	dest, err := safeio.OpenResolvedRoot(shown)
	if err != nil {
		return r, fmt.Errorf("export destination: %w", err)
	}
	defer dest.Close()
	stem := exportStem(r.Title, h.SHA256)
	for n := 1; n <= 20; n++ {
		name := stem + "." + h.Format
		if n > 1 {
			name = fmt.Sprintf("%s-%d.%s", stem, n, h.Format)
		}
		r.Path = filepath.Join(shown, name)
		if request.Reuse {
			same, err := identical(ctx, dest, name, h)
			if err == nil && same && request.ReadOnly {
				// A reused copy is reported read-only only once it is: the mode
				// is applied again, then the bytes are checked again after it.
				if same, err = reuseReadOnly(ctx, dest, name, h); err != nil {
					return r, err
				}
			}
			if err == nil && same {
				r.Reused = true
				return r, nil
			} else if err == nil {
				continue // A reader changed this copy; keep it and make another.
			} else if !errors.Is(err, os.ErrNotExist) {
				return r, err
			}
		}
		err := copyHolding(ctx, root, h, dest, name, request.ReadOnly)
		if errors.Is(err, os.ErrExist) {
			if request.Reuse {
				continue
			}
			return r, fmt.Errorf("%s already exists; export never overwrites", name)
		}
		if err != nil {
			return r, err
		}
		return r, nil
	}
	return r, errors.New("too many changed copies of this holding in the destination; remove some first")
}

// currentHolding returns the latest holding line under the shared reader lock.
func currentHolding(ctx context.Context, root, assetID string) (Holding, string, error) {
	state, err := LibraryStatus(ctx, root)
	if err != nil {
		return Holding{}, "", err
	}
	if state.Status != "initialized" {
		return Holding{}, "", errors.New("export requires an initialized library")
	}
	control, _, err := openControl(root, false)
	if err != nil {
		return Holding{}, "", err
	}
	defer control.close()
	_, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return Holding{}, "", err
	}
	h, ok := latestHoldings(lines)[assetID]
	if !ok {
		return Holding{}, "", errors.New("asset not present in managed holdings")
	}
	return h, state.LibraryID, nil
}

// exportStem is a portable file name from the declared title, never a path:
// letters, digits, and a few separators survive, everything else becomes a
// hyphen, and the hash prefix keeps names distinct.
func exportStem(title, hash string) string {
	var b strings.Builder
	for _, c := range title {
		switch {
		case unicode.IsLetter(c) || unicode.IsDigit(c):
			b.WriteRune(c)
		case c == ' ' || c == '-' || c == '_' || c == '.' || c == ',' || c == '/' || c == '\\':
			if b.Len() > 0 {
				b.WriteRune('-')
			}
		}
		if b.Len() >= 80 {
			break
		}
	}
	stem := strings.Trim(strings.Join(strings.FieldsFunc(b.String(), func(r rune) bool { return r == '-' }), "-"), "-.")
	if stem == "" {
		return hash[:16]
	}
	return stem + "-" + hash[:8]
}

// copyHolding streams the stored bytes into an exclusively created file,
// verifying size and SHA-256 before it is synced and kept.
func copyHolding(ctx context.Context, root string, h Holding, dest *os.Root, name string, readOnly bool) error {
	store, err := safeio.OpenRoot(root)
	if err != nil {
		return err
	}
	defer store.Close()
	osPath, err := assetOSPath(h.Path)
	if err != nil {
		return err
	}
	info, err := store.Lstat(osPath)
	if err != nil {
		return fmt.Errorf("stored file: %w; run library audit", err)
	}
	in, err := safeio.OpenRegular(store, osPath, info, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := safeio.EnsureSpaceIn(dest, h.Bytes); err != nil {
		return err
	}
	out, err := dest.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	sum := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, sum), io.LimitReader(importReader{ctx, in}, h.Bytes+1))
	syncErr, closeErr := out.Sync(), out.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil || n != h.Bytes || hex.EncodeToString(sum.Sum(nil)) != h.SHA256 {
		_ = dest.Remove(name)
		return errors.Join(err, errors.New("stored bytes do not match the holding; nothing was exported; run library audit"))
	}
	if readOnly {
		return dest.Chmod(name, 0o444)
	}
	return nil
}

// identical reports whether an existing copy still has exactly the holding's
// bytes. os.ErrNotExist means there is no copy under that name.
func identical(ctx context.Context, dest *os.Root, name string, h Holding) (bool, error) {
	info, err := dest.Lstat(name)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != h.Bytes {
		return false, nil
	}
	f, err := safeio.OpenRegular(dest, name, info, os.O_RDONLY)
	if err != nil {
		return false, err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, io.LimitReader(importReader{ctx, f}, h.Bytes+1)); err != nil {
		return false, err
	}
	return hex.EncodeToString(sum.Sum(nil)) == h.SHA256, nil
}

// reuseReadOnly removes write permission from an identical earlier copy and
// confirms its bytes did not change while it was still writable.
func reuseReadOnly(ctx context.Context, dest *os.Root, name string, h Holding) (bool, error) {
	info, err := dest.Lstat(name)
	if err != nil {
		return false, err
	}
	if info.Mode().Perm()&0o222 == 0 {
		return true, nil
	}
	if err := dest.Chmod(name, 0o444); err != nil {
		return false, err
	}
	return identical(ctx, dest, name, h)
}
