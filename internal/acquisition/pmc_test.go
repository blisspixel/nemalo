package acquisition

import (
	"context"
	"github.com/blisspixel/nemalo/internal/discovery"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pmcAcquisition(t *testing.T) (*fixtureProvider, Request) {
	t.Helper()
	p, r := fixture(), request(t)
	r.SourceID = "pmc:PMC13901.2"
	r.File = "PMC13901.2.pdf"
	p.e.ID = r.SourceID
	p.e.Source = "pmc"
	p.e.Access = discovery.PMCAccess
	p.e.Files[0].Name = r.File
	p.e.Files[0].Access = discovery.PMCPDFAccess
	p.e.Files[0].DownloadURL = "https://" + discovery.PMCHost + "/PMC13901.2/PMC13901.2.pdf"
	return p, r
}
func TestPMCAcquisitionIdentityEligibilityAndMD5(t *testing.T) {
	for _, change := range []func(*Request){func(r *Request) { r.SourceID = "pmc:PMC13901" }, func(r *Request) { r.File = "PMC13901.1.pdf" }} {
		p, r := pmcAcquisition(t)
		change(&r)
		if _, err := Acquire(context.Background(), p, r); err == nil || p.evaluations != 0 {
			t.Fatal("invalid selection reached provider", err)
		}
	}
	for name, change := range map[string]func(*fixtureProvider){
		"missing checksum":   func(p *fixtureProvider) { p.e.Files[0].MD5 = "" },
		"ineligible":         func(p *fixtureProvider) { p.e.Files[0].Access = "discovery_only" },
		"missing size":       func(p *fixtureProvider) { p.e.Files[0].Bytes = nil },
		"mismatched version": func(p *fixtureProvider) { p.e.ID = "pmc:PMC13901.7" },
	} {
		t.Run(name, func(t *testing.T) {
			p, r := pmcAcquisition(t)
			change(p)
			if _, err := Acquire(context.Background(), p, r); err == nil || p.downloads != 0 {
				t.Fatal("ineligible PDF downloaded", err)
			}
		})
	}
	p, r := pmcAcquisition(t)
	result, err := Acquire(context.Background(), p, r)
	if err != nil || !result.Complete || result.Intent.Request.SourceID != r.SourceID || result.Transfer.SHA256 == "" {
		t.Fatal(result, err)
	}
	p, r = pmcAcquisition(t)
	p.e.Files[0].MD5 = strings.Repeat("0", 32)
	result, err = Acquire(context.Background(), p, r)
	if err == nil || result.Complete {
		t.Fatal("MD5 mismatch accepted", result, err)
	}
	if _, err := os.Stat(filepath.Join(r.Output, "content.pdf")); err != nil {
		t.Fatal("failed bytes not retained", err)
	}
	if got := InspectPacket(r.Output); got.State == PacketResumable {
		t.Fatal("PMC packet marked resumable", got)
	}
}
