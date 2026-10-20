package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/blisspixel/nemalo/internal/safeio"
)

var errReadLimit = errors.New("byte_limit")

type inventoryFile interface {
	io.ReaderAt
	Stat() (fs.FileInfo, error)
}

type fileReader struct {
	ctx      context.Context
	f        io.ReaderAt
	limits   Limits
	report   *Report
	par2     *par2Run
	read     int64
	metadata int64
}

func readRegular(ctx context.Context, root *os.Root, name string, before fs.FileInfo, limits Limits, report *Report, run *par2Run) (sum string, metadata *PAR2File, observations []par2Observation, err error) {
	if err := ctx.Err(); err != nil {
		return "", nil, nil, err
	}
	f, err := safeio.OpenRegular(root, name, before, os.O_RDONLY)
	if err != nil {
		return "", nil, nil, err
	}
	sum, metadata, observations, err = readOpened(ctx, f, before, name, limits, report, run)
	return sum, metadata, observations, errors.Join(err, f.Close())
}

func (r *fileReader) readAt(p []byte, off int64, metadata bool) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > min(r.limits.FileBytes-r.read, r.limits.TotalBytes-r.report.BytesRead) {
		return 0, errReadLimit
	}
	if metadata && int64(len(p)) > min(par2FileBytes-r.metadata, par2TotalBytes-r.par2.report.BytesRead) {
		r.par2.stopped = int64(len(p)) > par2TotalBytes-r.par2.report.BytesRead
		return 0, errPAR2Limit
	}
	n, err := r.f.ReadAt(p, off)
	r.read += int64(n)
	r.report.BytesRead += int64(n)
	if metadata {
		r.metadata += int64(n)
		r.par2.report.BytesRead += int64(n)
	}
	return n, err
}

func readOpened(ctx context.Context, f inventoryFile, before fs.FileInfo, name string, limits Limits, report *Report, run *par2Run) (sum string, metadata *PAR2File, observations []par2Observation, err error) {
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Size() != before.Size() || !opened.ModTime().Equal(before.ModTime()) {
		return "", nil, nil, errors.Join(err, errors.New("file changed before reading"))
	}
	r := &fileReader{ctx: ctx, f: f, limits: limits, report: report, par2: run}
	if limits.PAR2 {
		metadata, observations, err = inspectPAR2(ctx, r, before.Size(), kind(name) == "recovery_data")
	}
	if limits.Hashes && ctx.Err() == nil {
		if before.Size() > min(limits.FileBytes-r.read, limits.TotalBytes-report.BytesRead) {
			if !errors.Is(err, errReadLimit) {
				err = errors.Join(err, errReadLimit)
			}
		} else {
			h := sha256.New()
			buf := make([]byte, 32<<10)
			var hashErr error
			for off := int64(0); off < before.Size(); {
				n, readErr := r.readAt(buf[:min(int64(len(buf)), before.Size()-off)], off, false)
				_, _ = h.Write(buf[:n])
				off += int64(n)
				if readErr != nil {
					hashErr = readErr
					break
				}
				if n == 0 {
					hashErr = io.ErrNoProgress
					break
				}
			}
			if hashErr == nil {
				sum = hex.EncodeToString(h.Sum(nil))
			} else {
				err = errors.Join(err, hashErr)
			}
		}
	}
	after, statErr := f.Stat()
	if statErr != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		if limits.PAR2 {
			metadata = par2Unknown("source_changed")
			metadata.State = "source_changed"
		}
		return "", metadata, nil, errors.Join(err, statErr, errors.New("file changed while reading"))
	}
	return sum, metadata, observations, err
}
