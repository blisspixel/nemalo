package app

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
)

type catalog struct{}

func (catalog) Search(_ context.Context, q string, limit, offset int) (discovery.Page, error) {
	return discovery.Page{Query: q, Offset: offset, Total: limit}, nil
}

type evaluator struct{ catalog }

func (evaluator) Evaluate(_ context.Context, id string) (discovery.Evaluation, error) {
	return discovery.Evaluation{ID: id}, nil
}

func TestProviderRegistryAndEvaluation(t *testing.T) {
	s := Service{Providers: map[string]Searcher{"archive": evaluator{}, "openlibrary": catalog{}}}
	if p, err := s.SearchSource(context.Background(), "archive", "books", 2, 1); err != nil || p.Query != "books" {
		t.Fatal(p, err)
	}
	if _, err := s.SearchSource(context.Background(), "missing", "books", 2, 1); err == nil {
		t.Fatal("unavailable provider accepted")
	}
	if e, err := s.Evaluate(context.Background(), "archive:demo"); err != nil || e.ID != "archive:demo" {
		t.Fatal(e, err)
	}
	for _, id := range []string{"demo", "openlibrary:OL1W", "missing:id"} {
		if _, err := s.Evaluate(context.Background(), id); err == nil {
			t.Fatal("unsupported evaluation accepted", id)
		}
	}
}

func TestSharedServices(t *testing.T) {
	s := Service{Providers: map[string]Searcher{"openlibrary": catalog{}}}
	p, err := s.Search(context.Background(), "books", 4, 3)
	if err != nil || p.Query != "books" || p.Total != 4 || p.Offset != 3 {
		t.Fatal(p, err)
	}
	r, err := s.Inspect(context.Background(), t.TempDir(), inventory.Defaults())
	if err != nil || !r.Complete {
		t.Fatal(r, err)
	}
	d := s.Doctor(config.Paths{State: "state"}, config.Config{Library: "library"}, func(name string) (string, error) {
		if name == "7z" {
			return "7z", nil
		}
		return "", errors.New("not installed")
	})
	if d.Security != "not_scanned" || len(d.Capabilities) != 5 || d.Capabilities[0].Available || !d.Capabilities[2].Available {
		t.Fatalf("tool availability became scan coverage: %+v", d)
	}
	bridge := d.Capabilities[4]
	if bridge.Name != "descriptor_bridge" || !bridge.Available {
		t.Fatal(bridge)
	}
	if d.ScannerRecommendation == "" || d.ChecksumRecommendation == "" || !strings.Contains(d.ChecksumRecommendation, "does not prove") {
		t.Fatal(d.ScannerRecommendation, d.ChecksumRecommendation)
	}
	switch runtime.GOOS {
	case "windows":
		if d.Capabilities[0].Recommended || !d.Capabilities[1].Recommended || !strings.Contains(d.ScannerRecommendation, "Microsoft Defender") {
			t.Fatal(d.Capabilities[:2], d.ScannerRecommendation)
		}
	case "darwin":
		if !d.Capabilities[0].Recommended || d.Capabilities[1].Recommended || !strings.Contains(d.ScannerRecommendation, "macOS does not provide") {
			t.Fatal(d.Capabilities[:2], d.ScannerRecommendation)
		}
	default:
		if !d.Capabilities[0].Recommended || d.Capabilities[1].Recommended || !strings.Contains(d.ScannerRecommendation, "including Omarchy") {
			t.Fatal(d.Capabilities[:2], d.ScannerRecommendation)
		}
	}
	if d.Capabilities[2].Recommended || d.Capabilities[3].Recommended {
		t.Fatal(d.Capabilities)
	}
	if New().Providers["openlibrary"] == nil {
		t.Fatal("missing catalog")
	}
	if _, err := Lookup("nemalo-nonexistent-fixture-tool"); err == nil {
		t.Fatal("unexpected tool")
	}
	_, _ = Lookup("MpCmdRun.exe")
}
