// Package pdftest builds small synthetic PDFs for tests. It is not used by
// production code.
package pdftest

import (
	"bytes"
	"fmt"
)

// Build writes a PDF with a classic cross-reference table. objects[i] is the
// body of object i+1; the document catalog must be object 1.
func Build(objects ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return b.Bytes()
}

// Stream is a stream object body with an exact /Length.
func Stream(dict, data string) string {
	return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
}

// Pages builds a document whose pages each show one line of Helvetica text.
// labels, when not empty, is the /PageLabels /Nums array body.
func Pages(labels string, texts ...string) []byte {
	kids := ""
	objects := []string{"", "", "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"}
	for _, text := range texts {
		page := len(objects) + 1
		kids += fmt.Sprintf("%d 0 R ", page)
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /Contents %d 0 R >>", page+1))
		objects = append(objects, Stream("", "BT /F1 12 Tf 72 700 Td ("+text+") Tj ET"))
	}
	catalog := "<< /Type /Catalog /Pages 2 0 R"
	if labels != "" {
		catalog += " /PageLabels << /Nums [" + labels + "] >>"
	}
	objects[0] = catalog + " >>"
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d /Resources << /Font << /F1 3 0 R >> >> >>", kids, len(texts))
	return Build(objects...)
}
