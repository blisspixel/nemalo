// Package transfer provides the bounded byte-transfer primitive shared by intake
// and the curated validation harness. It has no publication or rights policy.
package transfer

import (
	"context"
	"crypto/md5" // Source checksums establish transport consistency, not authenticity.
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

type Expectation struct {
	Bytes int64 // -1 means not declared.
	MD5   string
	SHA1  string
}

type Result struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// Copy counts and hashes even an unsuccessful transfer. At most limit+1 bytes
// are read/written: the extra byte detects a body exceeding the selected budget.
func Copy(ctx context.Context, dst io.Writer, src io.Reader, limit int64, expected Expectation) (Result, error) {
	var result Result
	if err := expected.Validate(limit); err != nil {
		return result, err
	}
	h, md, sh := sha256.New(), md5.New(), sha1.New()
	n, err := io.Copy(io.MultiWriter(dst, h, md, sh), io.LimitReader(contextReader{ctx, src}, limit+1))
	result.Bytes, result.SHA256 = n, hex.EncodeToString(h.Sum(nil))
	if err != nil {
		return result, err
	}
	if n == 0 || n > limit {
		return result, errors.New("empty download or transfer budget exceeded")
	}
	if expected.Bytes >= 0 && n != expected.Bytes {
		return result, errors.New("download differs from declared size")
	}
	if (expected.MD5 != "" && !equalHash(md.Sum(nil), expected.MD5)) || (expected.SHA1 != "" && !equalHash(sh.Sum(nil), expected.SHA1)) {
		return result, errors.New("download differs from source checksum")
	}
	return result, ctx.Err()
}

func (expected Expectation) Validate(limit int64) error {
	if limit < 1 || limit > 256<<20 || expected.Bytes < -1 || expected.Bytes > limit {
		return errors.New("invalid transfer limit or declared size exceeds budget")
	}
	for _, checksum := range []struct {
		value string
		size  int
	}{{expected.MD5, 16}, {expected.SHA1, 20}} {
		value, size := checksum.value, checksum.size
		if value != "" {
			b, err := hex.DecodeString(value)
			if err != nil || len(b) != size {
				return errors.New("invalid source checksum")
			}
		}
	}
	return nil
}

func equalHash(sum []byte, value string) bool {
	b, _ := hex.DecodeString(value)
	return string(sum) == string(b)
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

// Continue verifies a transfer that resumes after prefixLen bytes already
// written: the prefix is read and hashed (not written again), then src is
// copied to dst. The result and expectation cover the whole file, so a resumed
// file is checked exactly like one transferred at once.
func Continue(ctx context.Context, dst io.Writer, prefix io.Reader, prefixLen int64, src io.Reader, limit int64, expected Expectation) (Result, error) {
	var result Result
	if err := expected.Validate(limit); err != nil {
		return result, err
	}
	if prefixLen < 1 || prefixLen > limit {
		return result, errors.New("invalid resume offset")
	}
	h, md, sh := sha256.New(), md5.New(), sha1.New()
	n, err := io.Copy(io.MultiWriter(h, md, sh), io.LimitReader(contextReader{ctx, prefix}, prefixLen))
	if err != nil {
		return result, err
	}
	if n != prefixLen {
		return result, errors.New("retained partial bytes are shorter than recorded")
	}
	m, err := io.Copy(io.MultiWriter(dst, h, md, sh), io.LimitReader(contextReader{ctx, src}, limit+1-prefixLen))
	result.Bytes, result.SHA256 = n+m, hex.EncodeToString(h.Sum(nil))
	if err != nil {
		return result, err
	}
	if result.Bytes > limit {
		return result, errors.New("transfer budget exceeded")
	}
	if expected.Bytes >= 0 && result.Bytes != expected.Bytes {
		return result, errors.New("download differs from declared size")
	}
	if (expected.MD5 != "" && !equalHash(md.Sum(nil), expected.MD5)) || (expected.SHA1 != "" && !equalHash(sh.Sum(nil), expected.SHA1)) {
		return result, errors.New("download differs from source checksum")
	}
	return result, ctx.Err()
}
