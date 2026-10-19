package discovery

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzGutenbergRDF parses arbitrary bibliographic records. Only files of the
// requested text number may be offered for download, every offer points to
// the pinned mirror, and parsing is deterministic.
func FuzzGutenbergRDF(f *testing.F) {
	f.Add("1342", sampleRDF("1342"))
	f.Add("1342", sampleRDF("99"))
	f.Add("7", `<!DOCTYPE rdf [<!ENTITY x "y">]><rdf:RDF/>`)
	f.Add("7", "<rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\xff</rdf:RDF>")
	f.Fuzz(func(t *testing.T, number, body string) {
		if len(body) > 1<<16 || !ValidGutenbergID("gutenberg:"+number) {
			t.Skip()
		}
		parsed, err := parseGutenbergRDF(number, []byte(body))
		again, err2 := parseGutenbergRDF(number, []byte(body))
		if (err == nil) != (err2 == nil) || len(parsed.files) != len(again.files) {
			t.Fatal("non-deterministic record")
		}
		if err != nil {
			return
		}
		for _, file := range parsed.files {
			if file.DownloadURL == "" {
				continue
			}
			if !strings.HasPrefix(file.DownloadURL, "https://"+GutenbergMirrorHost+"/cache/epub/"+number+"/") || file.Access != GutenbergEPUBAccess {
				t.Fatalf("offered a file outside the pinned mirror or text number: %#v", file)
			}
			if file.Bytes == nil || *file.Bytes < 1 {
				t.Fatalf("offered a file without a positive extent: %#v", file)
			}
		}
	})
}

// FuzzCatalogRecord parses arbitrary catalog rows. A row that parses has a
// canonical text number, a title, valid UTF-8 everywhere, and no field over
// its limit.
func FuzzCatalogRecord(f *testing.F) {
	f.Add("1342,Text,1998-06-01,Pride and Prejudice,en,\"Austen, Jane\",Love stories,PR,Novels")
	f.Add("01,Text,,T,en,,,,")
	f.Add("5,Sound,2007-13-40,T,en; fr,,,,")
	f.Add("9,\"Te\nxt\",,T,,,,,")
	f.Fuzz(func(t *testing.T, line string) {
		if len(line) > 1<<16 {
			t.Skip()
		}
		r := csv.NewReader(strings.NewReader(line))
		r.FieldsPerRecord = 9
		fields, err := r.Read()
		if err != nil {
			return
		}
		rec, err := parseCatalogRecord(fields)
		if err != nil {
			return
		}
		if rec.num < 1 || !ValidGutenbergID("gutenberg:"+fields[0]) || rec.title == "" || strings.ContainsAny(rec.kind, "\r\n") {
			t.Fatalf("invalid accepted row %#v", rec)
		}
		for _, v := range append(append(append([]string{rec.title, rec.kind, rec.issued}, rec.authors...), rec.languages...), rec.subjects...) {
			if !utf8.ValidString(v) || strings.ContainsRune(v, 0) || len(v) > 16384 {
				t.Fatalf("invalid accepted field %q", v)
			}
		}
	})
}

// FuzzEdition parses arbitrary Open Library edition records. An accepted
// edition belongs to the requested work, names at most one Archive item,
// and that item is a valid Archive identifier.
func FuzzEdition(f *testing.F) {
	f.Add(`{"key":"/books/OL1M","title":"T","works":[{"key":"/works/OL1W"}],"ocaid":"item1"}`)
	f.Add(`{"key":"/books/OL1M","works":[{"key":"/works/OL2W"}]}`)
	f.Add(`{"key":"/books/OL1M","works":[{"key":"/works/OL1W"}],"ocaid":["a","b"]}`)
	f.Add(`null`)
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 1<<16 {
			t.Skip()
		}
		e, ok, err := parseEdition(json.RawMessage(raw), "openlibrary:OL1W")
		if err != nil || !ok {
			return
		}
		if !ValidOpenLibraryEdition(e.ID) {
			t.Fatalf("accepted an invalid edition id %q", e.ID)
		}
		if e.ArchiveID != "" && !ValidArchiveID(e.ArchiveID) {
			t.Fatalf("accepted an invalid Archive item %q", e.ArchiveID)
		}
	})
}
