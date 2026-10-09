package transfer

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCopyIntegrityAndLimits(t *testing.T) {
	body := []byte("bounded bytes")
	md, sh, sha := md5.Sum(body), sha1.Sum(body), sha256.Sum256(body)
	good := Expectation{int64(len(body)), hex.EncodeToString(md[:]), hex.EncodeToString(sh[:])}
	for _, tc := range []struct {
		name     string
		body     string
		limit    int64
		expected Expectation
		good     bool
	}{
		{"checksums", string(body), 100, good, true},
		{"unknown size", string(body), int64(len(body)), Expectation{Bytes: -1}, true},
		{"zero budget", string(body), 0, good, false},
		{"huge budget", string(body), 256<<20 + 1, good, false},
		{"invalid size", string(body), 100, Expectation{Bytes: -2}, false},
		{"declared over budget", string(body), 1, good, false},
		{"over body limit", string(body), 1, Expectation{Bytes: -1}, false},
		{"empty", "", 100, Expectation{Bytes: -1}, false},
		{"short", "short", 100, good, false},
		{"changed md5", string(body), 100, Expectation{Bytes: -1, MD5: strings.Repeat("0", 32)}, false},
		{"changed sha1", string(body), 100, Expectation{Bytes: -1, SHA1: strings.Repeat("0", 40)}, false},
		{"malformed md5", string(body), 100, Expectation{Bytes: -1, MD5: "bad"}, false},
		{"malformed sha1", string(body), 100, Expectation{Bytes: -1, SHA1: "xyz"}, false},
		{"same invalid checksums", string(body), 100, Expectation{Bytes: -1, MD5: good.SHA1, SHA1: good.SHA1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			r, err := Copy(context.Background(), &out, strings.NewReader(tc.body), tc.limit, tc.expected)
			if (err == nil) != tc.good || r.Bytes != int64(out.Len()) || (tc.limit > 0 && r.Bytes > tc.limit+1) {
				t.Fatal(r, err, out.Len())
			}
			if tc.good && (out.String() != string(body) || r.SHA256 != hex.EncodeToString(sha[:])) {
				t.Fatal("wrong delivered bytes", r)
			}
		})
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrShortWrite }

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestCopyFailureAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Copy(ctx, io.Discard, strings.NewReader("x"), 10, Expectation{Bytes: -1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := Copy(context.Background(), failWriter{}, strings.NewReader("x"), 10, Expectation{Bytes: -1}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	if _, err := Copy(context.Background(), io.Discard, failReader{}, 10, Expectation{Bytes: -1}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
}
