package staging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
)

var ErrSourceChanged = errors.New("archive snapshot does not match the planned source bytes")
var ErrSnapshotBudget = errors.New("archive source-hash allowance is exhausted")

// SnapshotIdentity describes the application-selected root bytes, not policy
// about their eligibility. Children already come from checked private copies.
type SnapshotIdentity struct {
	Bytes  int64
	SHA256 string
}

func copyArchiveBytes(ctx context.Context, limits Limits, name string, info fs.FileInfo, in io.Reader, out io.Writer) (int64, error) {
	r := io.LimitReader(contextReader{ctx, in}, info.Size()+1)
	if !limits.Coverage || limits.child || limits.ExpectedSources == nil {
		return io.Copy(out, r)
	}
	expected, ok := limits.ExpectedSources[name]
	if !ok || expected.Bytes != info.Size() || len(expected.SHA256) != 64 {
		return 0, ErrSourceChanged
	}
	if limits.SourceHashRemaining != nil && info.Size() >= *limits.SourceHashRemaining {
		return 0, ErrSnapshotBudget
	}
	sum := sha256.New()
	// Limit outside the budget reader so the permitted detection byte can
	// terminate normally at its boundary, even when it spends the allowance.
	checked := io.LimitReader(snapshotHashReader{contextReader{ctx, in}, limits.SourceHashRemaining}, info.Size()+1)
	n, err := io.Copy(io.MultiWriter(out, sum), checked)
	if err != nil {
		return n, err
	}
	if n != expected.Bytes || hex.EncodeToString(sum.Sum(nil)) != expected.SHA256 {
		return n, ErrSourceChanged
	}
	return n, nil
}

// Charge actual reads even if copying, cancellation or identity checks fail.
type snapshotHashReader struct {
	r         io.Reader
	remaining *int64
}

func (r snapshotHashReader) Read(p []byte) (int, error) {
	if r.remaining != nil {
		if *r.remaining <= 0 {
			return 0, ErrSnapshotBudget
		}
		if int64(len(p)) > *r.remaining {
			p = p[:int(*r.remaining)]
		}
	}
	n, err := r.r.Read(p)
	if r.remaining != nil {
		*r.remaining -= int64(n)
	}
	return n, err
}
