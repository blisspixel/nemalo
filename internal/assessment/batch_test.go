package assessment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type batchScanner struct {
	calls   *[][]string
	results func([]string) []scanner.Result
}

func (b batchScanner) Scan(context.Context, string) scanner.Result {
	panic("a batch scanner must receive whole batches")
}

func (b batchScanner) ScanMany(_ context.Context, files []string) []scanner.Result {
	*b.calls = append(*b.calls, append([]string{}, files...))
	return b.results(files)
}

func allClean(files []string) []scanner.Result {
	out := make([]scanner.Result, len(files))
	for i := range out {
		out[i] = scanner.Result{Status: "no_detections_reported"}
	}
	return out
}

func batchRoot(t *testing.T, n int) (*os.Root, []string, [][]byte) {
	t.Helper()
	dir := t.TempDir()
	names, contents := []string{}, [][]byte{}
	for i := range n {
		data := book(t, fmt.Sprintf(`<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Book %d</p></body></html>`, i), nil)
		name := fmt.Sprintf("book%02d.epub", i)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		names, contents = append(names, name), append(contents, data)
	}
	root, err := safeio.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root, names, contents
}

func scanAll(n int) []Options {
	options := make([]Options, n)
	for i := range options {
		options[i].Scan = true
	}
	return options
}

func TestCheckManyScansOnceAndBindsEachSnapshot(t *testing.T) {
	root, names, contents := batchRoot(t, 3)
	var calls [][]string
	av := batchScanner{&calls, func(files []string) []scanner.Result {
		for i, f := range files {
			got, err := os.ReadFile(f)
			if err != nil || !bytes.Equal(got, contents[i]) {
				t.Fatal("scanner did not receive the assessed snapshot bytes", i, err)
			}
		}
		out := allClean(files)
		out[1].Status = "findings"
		return out
	}}
	reports, errs := CheckManyInRoot(context.Background(), root, names, scanAll(3), av)
	if len(calls) != 1 || len(calls[0]) != 3 {
		t.Fatal("expected one scanner invocation for the batch", calls)
	}
	if errs[0] != nil || reports[0].Status != "limited_checks_passed" || reports[0].Antivirus.Status != "no_detections_reported" {
		t.Fatal(reports[0], errs[0])
	}
	if errs[1] == nil || reports[1].Status != "needs_review" || reports[1].Antivirus.Status != "findings" {
		t.Fatal("a detection must not pass", reports[1], errs[1])
	}
	for _, f := range calls[0] {
		if _, err := os.Stat(filepath.Dir(f)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("private snapshot retained", f, err)
		}
	}
	for i, name := range names {
		got, err := os.ReadFile(filepath.Join(root.Name(), name))
		if err != nil || !bytes.Equal(got, contents[i]) {
			t.Fatal("source mutated", name)
		}
	}
}

func TestCheckManyChunksBatchesAndIsolatesFailures(t *testing.T) {
	root, names, _ := batchRoot(t, maxBatchFiles+3)
	names = append(names, "missing.epub")
	options := scanAll(len(names))
	options[2].Scan = false
	var calls [][]string
	reports, errs := CheckManyInRoot(context.Background(), root, names, options, batchScanner{&calls, allClean})
	if len(calls) != 2 || len(calls[0]) != maxBatchFiles || len(calls[1]) != 2 {
		t.Fatal("batches not bounded to 32 files", len(calls))
	}
	if reports[2].Antivirus.Status != "not_scanned" || errs[2] != nil {
		t.Fatal("unscanned file was scanned", reports[2])
	}
	last := len(names) - 1
	if errs[last] == nil || reports[last].Status != "incomplete" {
		t.Fatal("missing file did not fail alone", reports[last], errs[last])
	}
	for i := range names[:last] {
		if errs[i] != nil {
			t.Fatal("one failure affected another file", i, errs[i])
		}
	}
}

func TestCheckManyRejectsUnboundOrMisalignedScans(t *testing.T) {
	root, names, _ := batchRoot(t, 2)
	var calls [][]string
	mutating := batchScanner{&calls, func(files []string) []scanner.Result {
		if err := os.WriteFile(files[0], []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
		return allClean(files)
	}}
	reports, errs := CheckManyInRoot(context.Background(), root, names, scanAll(2), mutating)
	if errs[0] == nil || reports[0].Status != "incomplete" || errs[1] != nil {
		t.Fatal("mutated snapshot was not detected, or affected its neighbor", reports, errs)
	}
	short := batchScanner{&calls, func([]string) []scanner.Result { return []scanner.Result{{Status: "no_detections_reported"}} }}
	reports, errs = CheckManyInRoot(context.Background(), root, names, scanAll(2), short)
	for i := range reports {
		if errs[i] == nil || reports[i].Antivirus.Status != "incomplete" {
			t.Fatal("misaligned batch results were attributed", i, reports[i])
		}
	}
	single := scanFunc(func(context.Context, string) scanner.Result { return scanner.Result{Status: "no_detections_reported"} })
	reports, errs = CheckManyInRoot(context.Background(), root, names, scanAll(2), single)
	if errs[0] != nil || errs[1] != nil || reports[1].Antivirus.Status != "no_detections_reported" {
		t.Fatal("single-file scanner fallback", reports, errs)
	}
	_, errs = CheckManyInRoot(context.Background(), root, names, scanAll(1), single)
	if errs[0] == nil || errs[1] == nil {
		t.Fatal("misaligned options accepted")
	}
}
