package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// flakyCatalog serves a catalog with Last-Modified, as gutenberg.org does,
// through http.ServeContent, which answers Range and If-Range. The first
// drops full or ranged responses are cut after cut bytes.
type flakyCatalog struct {
	mu       sync.Mutex
	body     string
	modified time.Time
	cut      int
	drops    int
	ranges   []string
	noDates  bool
	badRange bool
}

func (c *flakyCatalog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	body, drop, cut := c.body, c.drops > 0, c.cut
	if drop {
		c.drops--
	}
	if r.Header.Get("Range") != "" {
		c.ranges = append(c.ranges, r.Header.Get("Range")+" if "+r.Header.Get("If-Range"))
	}
	c.mu.Unlock()
	if c.badRange && r.Header.Get("Range") != "" {
		w.Header().Set("Content-Range", "bytes 0-1/2")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("Te"))
		return
	}
	modified := c.modified
	if c.noDates {
		modified = time.Time{}
		w.Header()["Date"] = nil
	}
	if drop {
		w = &cutWriter{ResponseWriter: w, left: cut}
	}
	http.ServeContent(w, r, "pg_catalog.csv", modified, strings.NewReader(body))
}

// cutWriter aborts the connection after left body bytes.
type cutWriter struct {
	http.ResponseWriter
	left int
}

func (c *cutWriter) Write(b []byte) (int, error) {
	if len(b) > c.left {
		_, _ = c.ResponseWriter.Write(b[:c.left])
		c.left = 0
		c.ResponseWriter.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}
	c.left -= len(b)
	return c.ResponseWriter.Write(b)
}

func quickCatalogRetries(t *testing.T) {
	saved := catalogPause
	catalogPause = func(int) time.Duration { return 0 }
	t.Cleanup(func() { catalogPause = saved })
}

func TestCatalogFetchContinuesADroppedDownloadExactly(t *testing.T) {
	quickCatalogRetries(t)
	c := &flakyCatalog{body: sampleCatalog, modified: time.Now().Add(-time.Hour), cut: 40, drops: 2}
	server := httptest.NewServer(c)
	t.Cleanup(server.Close)
	output := filepath.Join(t.TempDir(), "pg_catalog.csv")
	got, err := fetchCatalog(context.Background(), output, server.URL, server.Client())
	sum := sha256.Sum256([]byte(sampleCatalog))
	if err != nil || got.Continuations != 2 || got.SHA256 != hex.EncodeToString(sum[:]) || got.Bytes != int64(len(sampleCatalog)) {
		t.Fatal(got, err)
	}
	data, _ := os.ReadFile(output)
	if string(data) != sampleCatalog {
		t.Fatal("continued catalog differs")
	}
	if len(c.ranges) != 2 || !strings.HasPrefix(c.ranges[0], "bytes=40- if ") || !strings.HasPrefix(c.ranges[1], "bytes=80- if ") {
		t.Fatal(c.ranges)
	}
}

func TestCatalogFetchStartsOverWhenTheSourceChanged(t *testing.T) {
	quickCatalogRetries(t)
	c := &flakyCatalog{body: sampleCatalog, modified: time.Now().Add(-time.Hour), cut: 40, drops: 1}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			// A newer catalog: If-Range no longer matches, so the whole file comes.
			c.mu.Lock()
			c.body = sampleCatalog + "50,Text,2026-10-18,New Book,en,Someone,Topic,PZ,Shelf\n"
			c.modified = time.Now().Add(-time.Minute)
			c.mu.Unlock()
		}
		c.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	output := filepath.Join(t.TempDir(), "pg_catalog.csv")
	got, err := fetchCatalog(context.Background(), output, server.URL, server.Client())
	data, _ := os.ReadFile(output)
	if err != nil || got.Continuations != 1 || !strings.HasSuffix(string(data), "New Book,en,Someone,Topic,PZ,Shelf\n") || got.Bytes != int64(len(data)) {
		t.Fatal(got, err, len(data))
	}
}

func TestCatalogFetchRefusesWhatCannotBeContinuedSafely(t *testing.T) {
	quickCatalogRetries(t)
	for name, c := range map[string]*flakyCatalog{
		"no validator":   {body: sampleCatalog, cut: 40, drops: 1, noDates: true},
		"keeps dropping": {body: sampleCatalog, modified: time.Now().Add(-time.Hour), cut: 10, drops: 99},
		"wrong range":    {body: sampleCatalog, modified: time.Now().Add(-time.Hour), cut: 40, drops: 1, badRange: true},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(c)
			t.Cleanup(server.Close)
			output := filepath.Join(t.TempDir(), "pg_catalog.csv")
			if _, err := fetchCatalog(context.Background(), output, server.URL, server.Client()); err == nil {
				t.Fatal("accepted")
			}
			if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("a partial catalog was kept")
			}
		})
	}
}

func TestCatalogValidatorIsStrongOnly(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		h    http.Header
		want bool
	}{
		{http.Header{"Etag": {`"abc"`}}, true},
		{http.Header{"Etag": {`W/"abc"`}}, false},
		{http.Header{"Last-Modified": {now.Add(-time.Hour).Format(http.TimeFormat)}, "Date": {now.Format(http.TimeFormat)}}, true},
		// A date within a second of the response is weak.
		{http.Header{"Last-Modified": {now.Format(http.TimeFormat)}, "Date": {now.Format(http.TimeFormat)}}, false},
		{http.Header{"Last-Modified": {now.Add(-time.Hour).Format(http.TimeFormat)}}, false},
		{http.Header{}, false},
	} {
		if got := catalogValidator(&http.Response{Header: tc.h}) != ""; got != tc.want {
			t.Fatal(tc.h, got)
		}
	}
}
