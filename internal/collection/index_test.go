package collection

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode"
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

func TestIndexControlsAcrossAllProse(t *testing.T) {
	payload := "日本語 العربية café\x1b]52;c;payload\a\b\u009b\u009d\u202e\u2066\u2069\U000e0001"
	m := fixture()
	r := &m.Resources[0]
	r.Title, r.Rationale, r.Rights, r.Notes = payload, payload, payload, payload
	r.Authors, r.Languages = []string{payload}, []string{payload}
	report := Report{Results: []Result{{ResourceID: r.ID, Acquired: true, Assets: []Receipt{{Asset: Asset{Name: payload}, SHA256: payload}}}}}
	var out bytes.Buffer
	if err := WriteIndex(&out, m, report, payload); err != nil {
		t.Fatal(err)
	}
	for _, r := range out.String() {
		if unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && r != '\n' && r != '\t') {
			t.Fatalf("raw control in index: %U", r)
		}
	}
	if !strings.Contains(out.String(), "日本語 العربية café") || !strings.Contains(out.String(), `\\u001b`) {
		t.Fatal("visible evidence or multilingual text lost", out.String())
	}
	if m.Resources[0].Title != payload {
		t.Fatal("source metadata was altered")
	}
}
