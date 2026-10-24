package nativecheck

import (
	"archive/zip"
	"bytes"
)

func zipFixture(files [][2]string) ([]byte, error) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, f := range files {
		w, err := z.CreateHeader(&zip.FileHeader{Name: f[0], Method: zip.Store})
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(f[1])); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func bookFixture(body string) ([]byte, error) {
	return zipFixture([][2]string{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{"book.opf", `<package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Native synthetic fixture</dc:title><dc:language>en</dc:language></metadata><manifest><item id="text" href="text.xhtml" media-type="application/xhtml+xml"/><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/></manifest><spine><itemref idref="text"/></spine></package>`},
		{"text.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><body><p id="start">` + body + `</p></body></html>`},
		{"nav.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><body><nav epub:type="toc"><ol><li><a href="text.xhtml#start">Synthetic chapter</a></li></ol></nav></body></html>`},
	})
}
