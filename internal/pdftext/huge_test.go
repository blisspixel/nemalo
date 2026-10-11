package pdftext

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// guarded runs Extract and requires a budget refusal with little allocation.
func guarded(t *testing.T, name string, data []byte) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Extract(context.Background(), data, Defaults())
	runtime.ReadMemStats(&after)
	if alloc := after.TotalAlloc - before.TotalAlloc; !errors.Is(err, ErrBudget) || alloc > 16<<20 {
		t.Fatalf("%s: %v, %d MiB allocated", name, err, alloc>>20)
	}
}

const catalogObject = "1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"

// xrefFile writes prefix, the catalog, then body at startxref.
func xrefFile(prefix, body string) []byte {
	head := "%PDF-1.7\n" + prefix + catalogObject
	return []byte(head + body + fmt.Sprintf("\nstartxref\n%d\n%%%%EOF\n", len(head)))
}

// Regression: a cross-reference entry with a very high object number made the
// parser allocate hundreds of MiB for a file of a few hundred bytes, and the
// first guard could be bypassed by how the declaration was written.
func TestHighObjectNumbersStayBounded(t *testing.T) {
	entry := "0000000009 00000 n \n"
	for name, body := range map[string]string{
		"table":        "xref\n0 1\n0000000000 65535 f \n8388600 1\n" + entry + "trailer\n<< /Size 3 /Root 1 0 R >>",
		"comment":      "xref%c\n8388600 1\n" + entry + "trailer\n<< /Size 3 /Root 1 0 R >>",
		"size":         "xref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 8388601 /Root 1 0 R >>",
		"stream index": "3 0 obj\n<< /Type /XRef /Ind#65x [0 2 8000000 1] /W [1 2 1] /Size 4 /Root 1 0 R /Length 0 >>\nstream\n\nendstream\nendobj",
		"long index":   "3 0 obj\n<< /Type /XRef /Index [" + strings.Repeat("0 0 ", 1100) + "8000000 1] /W [1 2 1] /Size 4 /Root 1 0 R /Length 0 >>\nstream\n\nendstream\nendobj",
		"stream size":  "3 0 obj\n<< /Type /XRef /W [1 2 1] /Size 8000000 /Root 1 0 R /Length 0 >>\nstream\n\nendstream\nendobj",
	} {
		guarded(t, name, xrefFile("", body))
	}
	// /Prev leads back to an earlier table with the high number.
	early := "xref\n8388600 1\n" + entry + "trailer\n<< /Size 3 >>\n"
	guarded(t, "previous", xrefFile(early, fmt.Sprintf("xref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 3 /Root 1 0 R /Prev %d >>", len("%PDF-1.7\n"))))
	// An ordinary file, and /Size inside unrelated stream data, are accepted.
	if !xrefBounded(build("<< /Type /Catalog /Pages 2 0 R >>", stream("", "/Size 99999999")), 100) {
		t.Fatal("ordinary file refused")
	}
}
