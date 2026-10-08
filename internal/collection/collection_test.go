package collection

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
)

func fixture() Manifest {
	return Manifest{SchemaVersion: 1, Resources: []Resource{{ID: "sample", Title: "Title", Languages: []string{"en"}, Type: "article", Rationale: "Study", Rights: "Personal reading", RightsURL: "https://example.org/rights", Assets: []Asset{{Name: "sample.pdf", Format: "pdf", URL: "https://arxiv.org/pdf/1706.03762v7"}}}}}
}

func TestManifest(t *testing.T) {
	m := fixture()
	b, _ := json.Marshal(m)
	if _, err := Load(bytes.NewReader(b)); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"null", "{}", string(b) + " {}", strings.Replace(string(b), `"schema_version":1`, `"extra":1,"schema_version":1`, 1)} {
		if _, err := Load(strings.NewReader(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	mutations := []func(*Manifest){
		func(m *Manifest) { m.SchemaVersion = 2 }, func(m *Manifest) { m.Resources = nil },
		func(m *Manifest) { m.Resources = append(m.Resources, m.Resources[0]) },
		func(m *Manifest) { m.Resources[0].ID = "../escape" }, func(m *Manifest) { m.Resources[0].Title = "" },
		func(m *Manifest) { m.Resources[0].Type = "script" }, func(m *Manifest) { m.Resources[0].Assets[0].Name = "../sample.pdf" },
		func(m *Manifest) { m.Resources[0].Assets = append(m.Resources[0].Assets, m.Resources[0].Assets[0]) },
		func(m *Manifest) { m.Resources[0].Assets[0].Format = "exe" }, func(m *Manifest) { m.Resources[0].Assets[0].Name = "wrong.mp3" },
		func(m *Manifest) { m.Resources[0].Assets[0].URL = "http://arxiv.org/file" },
	}
	for i, mutate := range mutations {
		m := fixture()
		mutate(&m)
		if m.Validate() == nil {
			t.Errorf("mutation %d accepted", i)
		}
	}
	for _, raw := range []string{"https://archive.org/x", "https://www.archive.org/x", "https://ia800001.us.archive.org/x", "https://dn710904.ca.archive.org/x", "https://gutenberg.pglaf.org/x", "https://export.arxiv.org/x"} {
		if permittedURL(raw) != nil {
			t.Errorf("rejected %s", raw)
		}
	}
	for _, raw := range []string{"https://archive.org:443/x", "https://u:p@archive.org/x", "https://archive.org/x#frag", "https://archive.org.evil.test/x", "https://127.0.0.1/x", "://", "https://evil.us.archive.org/x", "https://iaevil.us.archive.org/x", "https://dn710904.ca.archive.org.evil.test/x"} {
		if permittedURL(raw) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	c := NewClient()
	request, _ := http.NewRequest(http.MethodGet, "https://example.org", nil)
	if c.CheckRedirect(request, nil) == nil {
		t.Fatal("external redirect accepted")
	}
	request, _ = http.NewRequest(http.MethodGet, "https://archive.org/x", nil)
	if c.CheckRedirect(request, nil) != nil || c.CheckRedirect(request, make([]*http.Request, 5)) == nil {
		t.Fatal("redirect count policy")
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testAcquirer(body string, status int, length int64) Acquirer {
	return Acquirer{Budget: 1 << 20, Client: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("User-Agent") == "" {
			return nil, errors.New("missing identification")
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), ContentLength: length, Request: r}, nil
	})}}
}

func TestAcquireAndReconcile(t *testing.T) {
	dir := t.TempDir()
	a := testAcquirer("%PDF-1.7\nfixture\n%%EOF", 200, -1)
	var progress bytes.Buffer
	r, err := a.Acquire(context.Background(), fixture(), dir, &progress)
	if err != nil || !r.Complete || r.AcquiredResources != 1 || r.TransferredBytes == 0 || r.Results[0].Assets[0].Antivirus != "not_scanned" {
		t.Fatalf("%+v %v", r, err)
	}
	if progress.Len() == 0 {
		t.Fatal("no progress")
	}
	r, err = a.Acquire(context.Background(), fixture(), dir, nil)
	if err != nil || r.TransferredBytes != 0 || r.StoredBytes == 0 {
		t.Fatalf("reconciliation: %+v %v", r, err)
	}
	name := filepath.Join(dir, "sample", "sample.pdf")
	if err := os.WriteFile(name, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err = a.Acquire(context.Background(), fixture(), dir, nil)
	if err == nil || r.Complete || len(r.Results[0].Errors) != 1 {
		t.Fatal("changed asset accepted")
	}
	if b, _ := os.ReadFile(name); string(b) != "changed" {
		t.Fatal("changed asset overwritten")
	}
}

func TestAcquireFailures(t *testing.T) {
	cases := []struct {
		body           string
		status         int
		length, budget int64
	}{
		{"bad", 200, -1, 100}, {"", 200, 0, 100}, {"%PDF-x %%EOF", 404, 11, 100},
		{"%PDF-x %%EOF", 200, 200, 100}, {"%PDF-x %%EOF", 200, 100, 100},
		{"%PDF-x %%EOF", 200, -1, 4},
	}
	for _, tc := range cases {
		a := testAcquirer(tc.body, tc.status, tc.length)
		a.Budget = tc.budget
		dir := t.TempDir()
		r, err := a.Acquire(context.Background(), fixture(), dir, nil)
		if err == nil || r.Complete {
			t.Fatal("invalid download accepted")
		}
		if _, err := os.Stat(filepath.Join(dir, "sample", "sample.pdf")); !os.IsNotExist(err) {
			t.Fatal("failed download published")
		}
		if _, err := os.Stat(filepath.Join(dir, "sample", "sample.pdf.partial")); !os.IsNotExist(err) {
			t.Fatal("partial left behind")
		}
	}
	for _, budget := range []int64{0, -1, 6 << 30} {
		a := testAcquirer("", 200, 0)
		a.Budget = budget
		if _, err := a.Acquire(context.Background(), fixture(), t.TempDir(), nil); err == nil {
			t.Fatal("invalid budget accepted")
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".collection.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := testAcquirer("", 200, 0).Acquire(context.Background(), fixture(), dir, nil); err == nil {
		t.Fatal("lock ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testAcquirer("", 200, 0).Acquire(ctx, fixture(), t.TempDir(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	a := testAcquirer("%PDF-x %%EOF", 200, -1)
	a.Interval = time.Hour
	ctx, cancel = context.WithCancel(context.Background())
	a.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: io.NopCloser(strings.NewReader("%PDF-x %%EOF")), Request: r}, nil
	})
	m := fixture()
	m.Resources[0].Assets = append(m.Resources[0].Assets, Asset{Name: "second.pdf", Format: "pdf", URL: "https://arxiv.org/pdf/1606.06565v2"})
	if _, err := a.Acquire(ctx, m, t.TempDir(), nil); !errors.Is(err, context.Canceled) {
		t.Fatal("operation not canceled")
	}
	dir = t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	a.nextRequest = time.Now().Add(time.Hour)
	ctx, cancel = context.WithCancel(context.Background())
	timer := time.AfterFunc(5*time.Millisecond, cancel)
	defer timer.Stop()
	if _, _, err := a.acquireAsset(ctx, root, "sample", fixture().Resources[0].Assets[0], 1000); !errors.Is(err, context.Canceled) {
		t.Fatalf("rate wait: %v", err)
	}
}

func TestReceiptFailures(t *testing.T) {
	for _, receipt := range []string{"", `{}`, `invalid`, strings.Repeat("x", 65<<10)} {
		dir := t.TempDir()
		sub := filepath.Join(dir, "sample")
		if err := os.Mkdir(sub, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "sample.pdf"), []byte("%PDF-x %%EOF"), 0600); err != nil {
			t.Fatal(err)
		}
		if receipt != "" {
			if err := os.WriteFile(filepath.Join(sub, "sample.pdf.receipt.json"), []byte(receipt), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := testAcquirer("", 200, 0).Acquire(context.Background(), fixture(), dir, nil); err == nil {
			t.Fatal("unreceipted file accepted")
		}
	}
	dir := t.TempDir()
	a := testAcquirer("%PDF-x %%EOF", 200, -1)
	if _, err := a.Acquire(context.Background(), fixture(), dir, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample", "sample.pdf"), []byte("%PDF-y %%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Acquire(context.Background(), fixture(), dir, nil); err == nil {
		t.Fatal("same-size hash change accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 500) }))
	defer server.Close()
	// Public manifests cannot redirect the fixture tool to localhost.
	m := fixture()
	m.Resources[0].Assets[0].URL = server.URL
	if _, err := a.Acquire(context.Background(), m, t.TempDir(), nil); err == nil {
		t.Fatal("local URL accepted")
	}
}

func epub(t *testing.T, mutate func(map[string]string)) []byte {
	t.Helper()
	files := map[string]string{
		"mimetype":               "application/epub+zip",
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><manifest><item id="chapter" href="chapter.xhtml"/></manifest><spine><itemref idref="chapter"/></spine></package>`,
		"chapter.xhtml":          "<html/>",
	}
	if mutate != nil {
		mutate(files)
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	if body, ok := files["mimetype"]; ok {
		f, err := w.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(f, body); err != nil {
			t.Fatal(err)
		}
		delete(files, "mimetype")
	}
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(f, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestInspect(t *testing.T) {
	b := epub(t, nil)
	c, err := assessment.Inspect(bytes.NewReader(b), int64(len(b)), "epub")
	if err != nil || c.ZIPCRC != "passed" || c.Members != 4 || c.Conformance != "not_checked" {
		t.Fatalf("%+v %v", c, err)
	}
	for _, tc := range []struct{ data, format string }{{"%PDF-1.7\n%%EOF", "pdf"}, {"ID3audio", "mp3"}, {"\xff\xfb1234", "mp3"}} {
		if _, err := assessment.Inspect(strings.NewReader(tc.data), int64(len(tc.data)), tc.format); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ data, format string }{{"", "pdf"}, {"bad", "pdf"}, {"%PDF-1.7", "pdf"}, {"html", "mp3"}, {"bad", "epub"}, {"PK\x03\x04bad", "epub"}, {"value", "exe"}} {
		if _, err := assessment.Inspect(strings.NewReader(tc.data), int64(len(tc.data)), tc.format); err == nil {
			t.Fatal("invalid content accepted")
		}
	}
	mutations := []func(map[string]string){
		func(f map[string]string) { f["../escape"] = "x" }, func(f map[string]string) { f[`C:\escape`] = "x" },
		func(f map[string]string) { f["mimetype"] = "bad" }, func(f map[string]string) { delete(f, "mimetype") },
		func(f map[string]string) { delete(f, "META-INF/container.xml") }, func(f map[string]string) { f["META-INF/container.xml"] = "<bad/>" },
		func(f map[string]string) {
			f["META-INF/container.xml"] = strings.ReplaceAll(f["META-INF/container.xml"], "book.opf", "../book.opf")
		},
		func(f map[string]string) { delete(f, "book.opf") }, func(f map[string]string) { f["book.opf"] = "<bad/>" },
		func(f map[string]string) {
			f["book.opf"] = strings.ReplaceAll(f["book.opf"], `idref="chapter"`, `idref="missing"`)
		},
		func(f map[string]string) { delete(f, "chapter.xhtml") }, func(f map[string]string) { f["book.opf"] = strings.ReplaceAll(f["book.opf"], `id="chapter"`, `id=""`) },
	}
	for i, mutate := range mutations {
		b := epub(t, mutate)
		if _, err := assessment.Inspect(bytes.NewReader(b), int64(len(b)), "epub"); err == nil {
			t.Errorf("mutation %d accepted", i)
		}
	}
	b = epub(t, func(f map[string]string) {
		f["book.opf"] = strings.ReplaceAll(f["book.opf"], "chapter.xhtml", "https://example.org/chapter")
	})
	c, err = assessment.Inspect(bytes.NewReader(b), int64(len(b)), "epub")
	if err != nil || !strings.Contains(strings.Join(c.Warnings, ";"), "remote manifest resource declared") {
		t.Fatal("remote resources not flagged")
	}
}

func TestContainerCorruptionAndBudgets(t *testing.T) {
	b := epub(t, nil)
	position := bytes.Index(b, []byte("application/epub+zip"))
	if position < 0 {
		t.Fatal("fixture has no stored mimetype")
	}
	b[position] ^= 1
	if _, err := assessment.Inspect(bytes.NewReader(b), int64(len(b)), "epub"); err == nil || !strings.Contains(err.Error(), "CRC") {
		t.Fatalf("corruption not detected: %v", err)
	}
	b = epub(t, nil)
	central := bytes.Index(b, []byte{'P', 'K', 1, 2})
	if central < 0 {
		t.Fatal("fixture has no central directory")
	}
	binary.LittleEndian.PutUint32(b[central+24:central+28], (32<<20)+1)
	if _, err := assessment.Inspect(bytes.NewReader(b), int64(len(b)), "epub"); err == nil || !strings.Contains(err.Error(), "expanded size") {
		t.Fatalf("oversized member accepted: %v", err)
	}
	for _, duplicate := range []bool{false, true} {
		var buffer bytes.Buffer
		w := zip.NewWriter(&buffer)
		header := &zip.FileHeader{Name: "member"}
		if !duplicate {
			header.SetMode(os.ModeSymlink | 0700)
		}
		f, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("target")); err != nil {
			t.Fatal(err)
		}
		if duplicate {
			if _, err := w.Create("member"); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := assessment.Inspect(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()), "epub"); err == nil {
			t.Fatal("link or duplicate accepted")
		}
	}
	if _, err := Load(strings.NewReader(strings.Repeat(" ", (2<<20)+1))); err == nil {
		t.Fatal("oversized manifest accepted")
	}
	m := fixture()
	m.Resources[0].Assets = append(m.Resources[0].Assets, Asset{Name: "second.pdf", URL: "https://arxiv.org/pdf/1606.06565v2", Format: "pdf"})
	body := "%PDF-x %%EOF"
	a := testAcquirer(body, 200, -1)
	a.Budget = int64(len(body))
	r, err := a.Acquire(context.Background(), m, t.TempDir(), nil)
	if err == nil || r.Complete || r.TransferredBytes != int64(len(body)) || len(r.Results[0].Assets) != 1 {
		t.Fatalf("aggregate transfer budget: %+v %v", r, err)
	}
}

func TestCuratedManifest(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "collections", "foundations.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	langs := map[string]bool{}
	for _, r := range m.Resources {
		counts[r.Type]++
		for _, lang := range r.Languages {
			langs[lang] = true
		}
		if r.Type == "audiobook" && len(r.Assets) < 2 {
			t.Fatal("recording lacks chapter set")
		}
	}
	if counts["ebook"] != 80 || counts["audiobook"] != 10 || counts["article"] != 10 || len(langs) < 10 {
		t.Fatalf("curation contract: %v, %d languages", counts, len(langs))
	}
	metadata, err := os.Open(filepath.Join("..", "..", "collections", "gutenberg-catalog.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	rows, err := csv.NewReader(metadata).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	textIDs := map[string]bool{}
	for _, row := range rows[1:] {
		if row[1] != "Text" {
			t.Fatalf("selected ebook is not cataloged text: %v", row[:2])
		}
		textIDs["pg-"+row[0]] = true
	}
	if len(textIDs) != 80 {
		t.Fatalf("catalog subset count: %d", len(textIDs))
	}
	for _, r := range m.Resources {
		if r.Type == "ebook" && !textIDs[r.ID] {
			t.Fatalf("ebook has no text catalog evidence: %s", r.ID)
		}
	}
}
