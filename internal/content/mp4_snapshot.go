package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/safeio"
)

var errPacketSnapshotChanged = errors.New("copied source bytes differ from the selected asset")

const packetSnapshotReadBytes = 80 << 20

// withPacketSnapshot binds every parser and payload read to the exact stream
// hashed while copied from the already-open source. Its working file is private,
// never published, never reopened and never written after verification.
func withPacketSnapshot(ctx context.Context, source io.ReaderAt, size int64, hash string, use func(io.ReaderAt) error) (err error) {
	return withMP4Snapshot(ctx, source, size, hash, func(snapshot *mp4Snapshot) error { return use(snapshot.reader) })
}

type mp4Snapshot struct {
	reader   io.ReaderAt
	file     *os.File
	path     string
	size     int64
	hash     string
	identity os.FileInfo
}

// withMP4Snapshot retains the verified handle. Only explicit decoder access
// uses its owned private filename, with a separate full revalidation afterwards.
func withMP4Snapshot(ctx context.Context, source io.ReaderAt, size int64, hash string, use func(*mp4Snapshot) error) (err error) {
	if size <= 0 || size > assessment.MaxMP4Bytes {
		return errBudget
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "nemalo-content-packets-")
	if err != nil {
		return err
	}
	var file *os.File
	defer func() {
		if file != nil {
			err = errors.Join(err, file.Close())
		}
		err = errors.Join(err, os.RemoveAll(dir))
	}()
	if err := safeio.EnsureSpace(dir, size); err != nil {
		return err
	}
	path := filepath.Join(dir, "asset")
	file, err = os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, h), contextReader{ctx, io.NewSectionReader(source, 0, size+1)})
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != hash {
		return errPacketSnapshotChanged
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	identity, err := file.Stat()
	if err != nil {
		return err
	}
	return use(&mp4Snapshot{reader: &packetSnapshotReader{ctx: ctx, reader: file, remaining: packetSnapshotReadBytes}, file: file, path: path, size: size, hash: hash, identity: identity})
}

// revalidate reads through the retained handle, never reopening the filename.
// The separate allowance is at most the expected source size plus one EOF probe.
func (s *mp4Snapshot) revalidate(ctx context.Context) error {
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx, io.NewSectionReader(s.file, 0, s.size+1)})
	if err != nil {
		return err
	}
	opened, err := s.file.Stat()
	if err != nil {
		return err
	}
	named, err := os.Lstat(s.path)
	if err != nil {
		return err
	}
	if n != s.size || hex.EncodeToString(h.Sum(nil)) != s.hash || !opened.Mode().IsRegular() || !named.Mode().IsRegular() || !os.SameFile(s.identity, opened) || !os.SameFile(opened, named) || opened.Size() != s.size || !opened.ModTime().Equal(s.identity.ModTime()) {
		return errPacketSnapshotChanged
	}
	return ctx.Err()
}

type packetSnapshotReader struct {
	ctx       context.Context
	reader    io.ReaderAt
	remaining int64
}

func (r *packetSnapshotReader) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		return 0, errBudget
	}
	n, err := r.reader.ReadAt(p, off)
	r.remaining -= int64(n)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
