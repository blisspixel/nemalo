package mcpserver

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/jobs"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type cleanScanner struct{}

func (cleanScanner) Scan(context.Context, string) scanner.Result {
	return scanner.Result{Status: "no_detections_reported", Tool: "test"}
}

// archiveStub offers one PDF and serves its bytes; nothing reaches a network.
type archiveStub struct{ body string }

func (a archiveStub) Search(context.Context, string, int, int) (discovery.Page, error) {
	return discovery.Page{}, nil
}

func (a archiveStub) Evaluate(_ context.Context, id string) (discovery.Evaluation, error) {
	n := int64(len(a.body))
	sum := md5.Sum([]byte(a.body))
	return discovery.Evaluation{Complete: true, ID: id, Source: "archive", Access: "no_item_restriction_declared", Title: []string{"A title"},
		Files: []discovery.OfferedFile{{Name: "paper.pdf", Bytes: &n, MD5: hex.EncodeToString(sum[:]), Access: "public_file_candidate", DownloadURL: "https://archive.org/download/demo/paper.pdf"}}}, nil
}

func (a archiveStub) Download(context.Context, discovery.OfferedFile) (*http.Response, error) {
	return &http.Response{StatusCode: 200, ContentLength: int64(len(a.body)), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(a.body))}, nil
}

func waitJob(t *testing.T, s interface {
	Get(string) (jobs.Job, error)
}, id string) jobs.Job {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		j, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status != jobs.Queued && j.Status != jobs.Running {
			return j
		}
	}
	t.Fatal("job did not finish")
	return jobs.Job{}
}

// Long operations start as durable jobs that finish after the call returns,
// within scope, and their state is read back through job_get and job_list.
func TestJobToolsRunAcquisitionAndRescanDurably(t *testing.T) {
	root := t.TempDir()
	if _, err := library.Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	intake := t.TempDir()
	store, err := jobs.Open(filepath.Join(t.TempDir(), "jobs"), 2, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close(); _ = store.Wait(context.Background()) })
	service := app.Service{Scanner: cleanScanner{}, Quarantine: filepath.Join(t.TempDir(), "quarantine"), Providers: map[string]app.Searcher{"archive": archiveStub{body: "%PDF-1.7\n%%EOF"}}}
	if names := writeToolNames(t, connect(t, service, Options{Scopes: []string{root, intake}, Write: true, Version: "test"})); slices.Contains(names, "job_get") {
		t.Fatal("job tools without a store", names)
	}
	s := connect(t, service, Options{Scopes: []string{root, intake}, Write: true, Network: true, Jobs: store, Version: "test"})
	names := writeToolNames(t, s)
	for _, want := range []string{"acquisition_start", "library_rescan_start", "job_get", "job_list", "job_cancel"} {
		if !slices.Contains(names, want) {
			t.Fatal("missing", want, names)
		}
	}
	for name, args := range map[string]map[string]any{
		"outside scope": {"source_id": "archive:demo", "file": "paper.pdf", "output": filepath.Join(t.TempDir(), "packet")},
		"both kinds":    {"source_id": "archive:demo", "file": "paper.pdf", "recording_format": "64Kbps MP3", "output": filepath.Join(intake, "packet")},
		"no name":       {"source_id": "archive:demo", "file": "paper.pdf", "output": intake + string(filepath.Separator) + ".."},
		"gutenberg":     {"source_id": "gutenberg:1342", "file": "1342.epub.noimages", "output": filepath.Join(intake, "gutenberg")},
	} {
		if res, _ := call(t, s, "acquisition_start", args); !res.IsError {
			t.Fatal(name, "started")
		}
	}
	res, env := call(t, s, "acquisition_start", map[string]any{"source_id": "archive:demo", "file": "paper.pdf", "output": filepath.Join(intake, "packet")})
	if res.IsError {
		t.Fatal(env)
	}
	id := env["data"].(map[string]any)["id"].(string)
	j := waitJob(t, store, id)
	if j.Status != jobs.Succeeded || j.Kind != "acquire" || j.Result.(map[string]any)["complete"] != true {
		t.Fatalf("%+v", j)
	}
	if _, err := os.Stat(filepath.Join(intake, "packet", "receipt.json")); err != nil {
		t.Fatal("the job did not write its packet", err)
	}
	_, env = call(t, s, "job_get", map[string]any{"id": id})
	if env["data"].(map[string]any)["status"] != jobs.Succeeded {
		t.Fatal(env)
	}
	if res, _ := call(t, s, "job_cancel", map[string]any{"id": id}); !res.IsError {
		t.Fatal("a finished job was cancelled")
	}
	if _, err := library.Import(context.Background(), library.ImportRequest{Library: root, Source: filepath.Join(intake, "packet"), Apply: true}); err != nil {
		t.Fatal(err)
	}
	_, env = call(t, s, "library_rescan_start", map[string]any{"library": root})
	rescan := waitJob(t, store, env["data"].(map[string]any)["id"].(string))
	if rescan.Status != jobs.Succeeded || rescan.Kind != "library_rescan" {
		t.Fatalf("%+v", rescan)
	}
	_, env = call(t, s, "job_list", map[string]any{})
	if listed, _ := env["data"].([]any); len(listed) != 2 {
		t.Fatal(env)
	}
	if res, _ := call(t, s, "job_get", map[string]any{"id": "job:nothing"}); !res.IsError {
		t.Fatal("an unknown job was found")
	}
	// A server with other scopes sees none of these jobs.
	elsewhere := connect(t, service, Options{Scopes: []string{t.TempDir()}, Write: true, Jobs: store, Version: "test"})
	if _, env := call(t, elsewhere, "job_list", map[string]any{}); len(env["data"].([]any)) != 0 {
		t.Fatal("jobs outside scope were listed", env)
	}
	if res, _ := call(t, elsewhere, "job_get", map[string]any{"id": id}); !res.IsError {
		t.Fatal("a job outside scope was read")
	}
}

// writeToolNames lists a write server's tools. Job tools are not
// destructive and not idempotent, and only acquisition reaches the network.
func writeToolNames(t *testing.T, s interface {
	ListTools(context.Context, *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
}) []string {
	t.Helper()
	list, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range list.Tools {
		a := tool.Annotations
		if strings.HasPrefix(tool.Name, "job_") || strings.HasSuffix(tool.Name, "_start") {
			network := tool.Name == "acquisition_start"
			if a == nil || a.ReadOnlyHint || a.IdempotentHint || a.DestructiveHint == nil || *a.DestructiveHint || a.OpenWorldHint == nil || *a.OpenWorldHint != network {
				t.Fatal("job tool annotations", tool.Name, a)
			}
		}
		names = append(names, tool.Name)
	}
	return names
}
