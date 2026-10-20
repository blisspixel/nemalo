package staging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"testing"
)

func TestCoveredZIPSnapshotBindsBytesAndSharesSourceHashWork(t *testing.T) {
	data := archive(t, []member{{name: "book.epub", body: []byte("book")}})
	for _, kind := range []string{"exact", "different_hash", "different_size", "missing", "budget", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			src, dst, _ := roots(t, data)
			sum := sha256.Sum256(data)
			limits := Defaults()
			limits.Coverage = true
			limits.ExpectedSources = map[string]SnapshotIdentity{"a.zip": {Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}}
			remaining := int64(len(data) + 1)
			limits.SourceHashRemaining = &remaining
			switch kind {
			case "different_hash":
				expected := limits.ExpectedSources["a.zip"]
				expected.SHA256 = hex.EncodeToString(make([]byte, 32))
				limits.ExpectedSources["a.zip"] = expected
			case "different_size":
				expected := limits.ExpectedSources["a.zip"]
				expected.Bytes++
				limits.ExpectedSources["a.zip"] = expected
			case "missing":
				delete(limits.ExpectedSources, "a.zip")
			case "budget":
				remaining = int64(len(data))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			saved := SevenZip
			SevenZip = func() (string, error) {
				t.Fatal("ordinary outer ZIP snapshot required an installed reader")
				return "", nil
			}
			t.Cleanup(func() { SevenZip = saved })
			x, err := ExtractZip(ctx, src, "a.zip", dst, books, limits)
			switch kind {
			case "exact":
				if err != nil || !x.Whole || !x.Complete || len(x.Members) != 1 || remaining != 1 {
					t.Fatal(x, err, remaining)
				}
			case "budget":
				if !errors.Is(err, ErrSnapshotBudget) || remaining != int64(len(data)) {
					t.Fatal(err, remaining)
				}
			case "cancelled":
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			default:
				if !errors.Is(err, ErrSourceChanged) || len(x.Members) != 0 {
					t.Fatal(x, err)
				}
			}
			if kind == "different_hash" && remaining != 1 {
				t.Fatal("failed copy hash work was refunded", remaining)
			}
			got, _ := src.ReadFile("a.zip")
			if !bytes.Equal(got, data) {
				t.Fatal("source mutated")
			}
			if kind != "exact" {
				entries, _ := os.ReadDir(dst.Name())
				if len(entries) != 0 {
					t.Fatal("unverified snapshot extracted members", entries)
				}
			}
		})
	}
}

type failingSnapshotRead struct{}

func (failingSnapshotRead) Read(p []byte) (int, error) {
	return copy(p, []byte("read before failure")), io.ErrUnexpectedEOF
}

func TestSnapshotSourceHashChargesBytesReturnedWithAnIOError(t *testing.T) {
	src, _, _ := roots(t, bytes.Repeat([]byte("x"), 100))
	info, err := src.Lstat("a.zip")
	if err != nil {
		t.Fatal(err)
	}
	limits := Defaults()
	limits.Coverage = true
	limits.ExpectedSources = map[string]SnapshotIdentity{"a.zip": {Bytes: 100, SHA256: hex.EncodeToString(make([]byte, 32))}}
	remaining := int64(101)
	limits.SourceHashRemaining = &remaining
	var out bytes.Buffer
	n, err := copyArchiveBytes(context.Background(), limits, "a.zip", info, failingSnapshotRead{}, &out)
	if !errors.Is(err, io.ErrUnexpectedEOF) || n != 19 || remaining != 82 || out.Len() != 19 {
		t.Fatal(n, err, remaining, out.Len())
	}
}

func TestSnapshotGrowthDetectionChargesTheLastByteWithoutInventingABudgetFailure(t *testing.T) {
	src, _, _ := roots(t, bytes.Repeat([]byte("x"), 100))
	info, err := src.Lstat("a.zip")
	if err != nil {
		t.Fatal(err)
	}
	limits := Defaults()
	limits.Coverage = true
	sum := sha256.Sum256(bytes.Repeat([]byte("x"), 100))
	limits.ExpectedSources = map[string]SnapshotIdentity{"a.zip": {Bytes: 100, SHA256: hex.EncodeToString(sum[:])}}
	remaining := int64(101)
	limits.SourceHashRemaining = &remaining
	var out bytes.Buffer
	n, err := copyArchiveBytes(context.Background(), limits, "a.zip", info, bytes.NewReader(bytes.Repeat([]byte("x"), 101)), &out)
	if !errors.Is(err, ErrSourceChanged) || n != 101 || remaining != 0 {
		t.Fatal("growth detection was misreported or uncharged", n, err, remaining)
	}
}
