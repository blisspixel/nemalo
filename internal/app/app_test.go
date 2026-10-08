package app

import (
	"context"
	"errors"
	"testing"

	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
)

type catalog struct{}

func (catalog) Search(_ context.Context, q string, limit, offset int) (discovery.Page, error) {
	return discovery.Page{Query: q, Offset: offset, Total: limit}, nil
}

func TestSharedServices(t *testing.T) {
	s := Service{Catalog: catalog{}}
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
	if d.Security != "not_scanned" || len(d.Capabilities) != 4 || d.Capabilities[0].Available || !d.Capabilities[2].Available {
		t.Fatalf("tool availability became scan coverage: %+v", d)
	}
	if New().Catalog == nil {
		t.Fatal("missing catalog")
	}
	if _, err := Lookup("nemalo-nonexistent-fixture-tool"); err == nil {
		t.Fatal("unexpected tool")
	}
	_, _ = Lookup("MpCmdRun.exe")
}
