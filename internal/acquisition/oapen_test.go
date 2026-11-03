package acquisition

import (
	"context"
	"github.com/blisspixel/nemalo/internal/discovery"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func oapenAcquisition(t *testing.T) (*fixtureProvider, Request) {
	t.Helper()
	p, r := fixture(), request(t)
	r.SourceID = "oapen:20.500.12657/25287"
	r.File = "9789085551201_OA_version.pdf"
	r.MaxBytes = 1 << 20
	p.e.ID = r.SourceID
	p.e.Source = "oapen"
	p.e.Access = discovery.OAPENAccess
	p.e.Files[0].Name = r.File
	p.e.Files[0].Access = discovery.OAPENOriginalAccess
	p.e.Files[0].DownloadURL = "https://" + discovery.OAPENHost + "/rest/bitstreams/4ca2f021-851f-483a-af4e-efd1b86654ba/retrieve"
	return p, r
}

func TestOAPENAcquisitionChecksTheOriginalAndCannotResume(t *testing.T) {
	for _, change := range []func(*Request){
		func(r *Request) { r.SourceID = "oapen:20.500.12854/26303" },
		func(r *Request) { r.File = "notes.txt" },
		func(r *Request) { r.File = "../book.pdf" },
	} {
		p, r := oapenAcquisition(t)
		change(&r)
		if _, err := Acquire(context.Background(), p, r); err == nil || p.evaluations != 0 {
			t.Fatal("invalid selection reached provider", err)
		}
	}
	for name, change := range map[string]func(*fixtureProvider){
		"missing checksum": func(p *fixtureProvider) { p.e.Files[0].MD5 = "" },
		"ineligible":       func(p *fixtureProvider) { p.e.Files[0].Access = "not_offered" },
		"missing size":     func(p *fixtureProvider) { p.e.Files[0].Bytes = nil },
		"publisher url": func(p *fixtureProvider) {
			p.e.Files[0].DownloadURL = "https://publisher.example/book.pdf"
		},
		"other host": func(p *fixtureProvider) {
			p.e.Files[0].DownloadURL = "https://directory.doabooks.org/rest/bitstreams/4ca2f021-851f-483a-af4e-efd1b86654ba/retrieve"
		},
		"mismatched handle": func(p *fixtureProvider) { p.e.ID = "oapen:20.500.12657/1" },
	} {
		t.Run(name, func(t *testing.T) {
			p, r := oapenAcquisition(t)
			change(p)
			if _, err := Acquire(context.Background(), p, r); err == nil || p.downloads != 0 {
				t.Fatal("ineligible file downloaded", err)
			}
		})
	}
	p, r := oapenAcquisition(t)
	n := int64(12)
	p.e.Files = append(p.e.Files, discovery.OfferedFile{Name: "book.epub", Format: "application/epub+zip", Access: discovery.OAPENOriginalAccess, Bytes: &n, MD5: p.e.Files[0].MD5, DownloadURL: "https://" + discovery.OAPENHost + "/rest/bitstreams/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/retrieve"})
	if got := Eligible(p.e, r.MaxBytes); len(got) != 2 || got[0].Format != "pdf" || got[1].Format != "epub" {
		t.Fatal(got)
	}
	result, err := Acquire(context.Background(), p, r)
	if err != nil || !result.Complete || result.Intent.Request.SourceID != r.SourceID || result.Transfer.SHA256 == "" || !strings.HasSuffix(result.ContentPath, "content.pdf") {
		t.Fatal(result, err)
	}
	p, r = oapenAcquisition(t)
	p.e.Files[0].MD5 = strings.Repeat("0", 32)
	result, err = Acquire(context.Background(), p, r)
	if err == nil || result.Complete {
		t.Fatal("MD5 mismatch accepted", result, err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, "content.pdf")); err != nil {
		t.Fatal("failed bytes not retained", err)
	}
	if got := InspectPacket(r.Output); got.State == PacketResumable {
		t.Fatal("OAPEN packet marked resumable", got)
	}
	p, r = oapenAcquisition(t)
	r.Resume = true
	if _, err := Acquire(context.Background(), p, r); err == nil || p.evaluations != 0 || p.downloads != 0 {
		t.Fatal("resume started a transfer", err)
	}
}
