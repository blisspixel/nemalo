package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

type guardedCollectionShelf struct {
	*shelf
	evaluations int
}

func (p *guardedCollectionShelf) Evaluate(ctx context.Context, id string) (discovery.Evaluation, error) {
	p.evaluations++
	return p.shelf.Evaluate(ctx, id)
}

func collectionPlanFixture(t *testing.T) (Service, *guardedCollectionShelf, CollectionAcquireRequest) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "library")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "collection.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"name":"Reviewed","items":[{"source":"archive:one","file":"one.pdf"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	p := &guardedCollectionShelf{shelf: &shelf{bodies: map[string][]byte{"archive:one": pdftest.Pages("", "Exact selection.")}}}
	s := Service{Providers: map[string]Searcher{"archive": p}, Scanner: citationNoScan{t}}
	return s, p, CollectionAcquireRequest{Library: root, Collection: path, Intake: filepath.Join(t.TempDir(), "missing"), MaxTotalBytes: 1 << 20, NoScan: true}
}

func TestCollectionPlanRefusesChangedReviewBeforeRequestsOrIntakeWrites(t *testing.T) {
	for _, change := range []string{"bytes", "collection_path", "library_identity", "packet", "total_budget", "recording_budget", "scanner", "detection", "held_selection", "invalid_plan"} {
		t.Run(change, func(t *testing.T) {
			s, p, req := collectionPlanFixture(t)
			preview, err := s.CollectionAcquire(context.Background(), req)
			if err != nil || len(preview.PlanDigest) != 64 || p.evaluations != 0 {
				t.Fatal(preview, err)
			}
			if _, err := os.Stat(req.Intake); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("preview created intake", err)
			}
			req.Apply, req.Plan = true, preview.PlanDigest
			switch change {
			case "bytes":
				f, err := os.OpenFile(req.Collection, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.WriteString("\n"); err != nil {
					t.Fatal(err)
				}
				if err = f.Close(); err != nil {
					t.Fatal(err)
				}
			case "collection_path":
				b, err := os.ReadFile(req.Collection)
				if err != nil {
					t.Fatal(err)
				}
				req.Collection = filepath.Join(t.TempDir(), "copy.json")
				if err = os.WriteFile(req.Collection, b, 0600); err != nil {
					t.Fatal(err)
				}
			case "library_identity":
				if err := os.Rename(req.Library, req.Library+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(req.Library, 0700); err != nil {
					t.Fatal(err)
				}
				if _, err := library.Initialize(context.Background(), req.Library); err != nil {
					t.Fatal(err)
				}
			case "packet":
				if err := os.MkdirAll(preview.Steps[0].Packet, 0700); err != nil {
					t.Fatal(err)
				}
			case "total_budget":
				req.MaxTotalBytes++
			case "recording_budget":
				req.MaxRecordingBytes = 2 << 30
			case "scanner":
				req.NoScan = false
			case "detection":
				req.OnDetection = "quarantine"
			case "held_selection":
				other := req
				other.Plan, other.Intake = "", filepath.Join(t.TempDir(), "other")
				if r, err := s.CollectionAcquire(context.Background(), other); err != nil || !r.Complete {
					t.Fatal(r, err)
				}
				if r, err := s.Ingest(context.Background(), library.IngestRequest{Library: req.Library, Folder: other.Intake, Apply: true}); err != nil || !r.Complete {
					t.Fatal(r, err)
				}
			case "invalid_plan":
				req.Plan = "not a reviewed digest"
			}
			before := p.evaluations
			r, err := s.CollectionAcquire(context.Background(), req)
			if err == nil || !strings.Contains(err.Error(), "preview changed") || r.Applied || p.evaluations != before {
				t.Fatal("stale preview reached acquisition", r, err, p.evaluations, before)
			}
			if change == "packet" {
				entries, err := os.ReadDir(preview.Steps[0].Packet)
				if err != nil || len(entries) != 0 {
					t.Fatal("retained packet changed", entries, err)
				}
			} else if _, err := os.Stat(req.Intake); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("refusal created intake", err)
			}
		})
	}
}

func TestCollectionMatchingPlanAppliesAndLaterPacketChangeRequiresNewReview(t *testing.T) {
	s, p, req := collectionPlanFixture(t)
	preview, err := s.CollectionAcquire(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	// The default and explicit equivalent recording budget describe one plan.
	req.MaxRecordingBytes = defaultRecordingBytes
	again, err := s.CollectionAcquire(context.Background(), req)
	if err != nil || again.PlanDigest != preview.PlanDigest {
		t.Fatal("unstable normalized plan", again, err)
	}
	req.Apply, req.Plan = true, preview.PlanDigest
	r, err := s.CollectionAcquire(context.Background(), req)
	if err != nil || !r.Applied || !r.Complete || r.PlanDigest != preview.PlanDigest || p.evaluations != 1 || len(p.downloads) != 1 {
		t.Fatal(r, err, p.evaluations, p.downloads)
	}
	r, err = s.CollectionAcquire(context.Background(), req)
	if err == nil || r.Applied || p.evaluations != 1 || len(p.downloads) != 1 {
		t.Fatal("old plan reused after receipt", r, err)
	}
	req.Apply, req.Plan = false, ""
	waiting, err := s.CollectionAcquire(context.Background(), req)
	if err != nil || waiting.Steps[0].Action != StepAwaitingImport {
		t.Fatal(waiting, err)
	}
	req.Apply, req.Plan = true, waiting.PlanDigest
	r, err = s.CollectionAcquire(context.Background(), req)
	if err != nil || !r.Complete || p.evaluations != 1 || len(p.downloads) != 1 {
		t.Fatal("awaiting import redownloaded", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req.Intake = filepath.Join(t.TempDir(), "cancelled")
	if r, err = s.CollectionAcquire(ctx, req); !errors.Is(err, context.Canceled) || r.Applied {
		t.Fatal(r, err)
	}
	if _, err = os.Stat(req.Intake); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled call created intake", err)
	}
}

func TestCollectionPlanKeepsDistinctRawIntakePathBytes(t *testing.T) {
	s, p, req := collectionPlanFixture(t)
	base := req.Intake
	req.Intake = base + "\xff"
	one, err := s.CollectionAcquire(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.Intake = base + "\xfe"
	two, err := s.CollectionAcquire(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if one.PlanDigest == two.PlanDigest {
		t.Fatal("different raw intake paths collapsed to the same review binding")
	}
	req.Plan, req.Apply = one.PlanDigest, true
	r, err := s.CollectionAcquire(context.Background(), req)
	if err == nil || r.Applied || p.evaluations != 0 {
		t.Fatal("raw path change reached acquisition", r, err, p.evaluations)
	}
	if _, err := os.Stat(req.Intake); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("raw path refusal created intake", err)
	}
}
