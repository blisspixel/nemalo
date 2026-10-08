package collection

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestIndex(t *testing.T) {
	m := fixture()
	m.Resources[0].Title = "<script> [link](bad) *title*\n# heading"
	m.Resources[0].Notes = "A discrepancy"
	r := Report{AcquiredResources: 1, Results: []Result{{ResourceID: "sample", Acquired: true, Assets: []Receipt{{Asset: m.Resources[0].Assets[0], Bytes: 10, SHA256: strings.Repeat("a", 64)}}}}}
	var b bytes.Buffer
	if err := WriteIndex(&b, m, r, "intake folder"); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if strings.Contains(s, "<script>") || strings.Contains(s, "\n# heading") || !strings.Contains(s, "intake%20folder/sample/sample.pdf") || !strings.Contains(s, "1 of 1") || !strings.Contains(s, "A discrepancy") {
		t.Fatalf("unsafe or incomplete index: %s", s)
	}
	b.Reset()
	if err := WriteIndex(&b, m, Report{}, "intake"); err != nil || !strings.Contains(b.String(), "Acquisition incomplete") {
		t.Fatal("missing status")
	}
	if err := WriteIndex(failingWriter{}, m, r, "intake"); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("write error swallowed")
	}
}
