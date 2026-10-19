package discovery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func librivoxProvider(t *testing.T, handler http.HandlerFunc) (*LibriVox, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	p := NewLibriVox()
	p.endpoint, p.client = server.URL, server.Client()
	return p, &calls
}

// projectJSON is a LibriVox record shaped like the API's extended output.
func projectJSON(archive string) string {
	return `{"books":[{"id":"119","title":"Art of War","language":"English","url_librivox":"https://librivox.org/the-art-of-war-by-sun-tzu/","url_iarchive":"` + archive + `","sections":[` +
		`{"section_number":"1","title":"Chapter one","playtime":"506","readers":[{"reader_id":"209","display_name":"Moira Fogarty"}]},` +
		`{"section_number":"2","title":"Chapter two","playtime":"466","readers":[{"reader_id":"209","display_name":"Moira Fogarty"},{"reader_id":"964","display_name":"Paul Sze"}]}]}]}`
}

func linkedFixture() map[string]any {
	f := librivoxFixture()
	md := f["metadata"].(map[string]any)
	md["collection"] = []any{"librivoxaudio", "audio_bookspoetry"}
	md["call_number"] = "119"
	return f
}

func evaluateLinked(t *testing.T, item map[string]any, lv *LibriVox) Evaluation {
	t.Helper()
	p := archiveProvider(t, func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(item) })
	p.LibriVox = lv
	e, err := p.Evaluate(context.Background(), "archive:demo")
	if err != nil || !e.Complete {
		t.Fatal(e, err)
	}
	return e
}

func TestLibriVoxProjectIsLinkedOnlyWhenItNamesTheItemBack(t *testing.T) {
	lv, calls := librivoxProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("id") != "119" || q.Get("format") != "json" || q.Get("extended") != "1" {
			t.Error("bad project request", r.URL)
		}
		_, _ = io.WriteString(w, projectJSON("https://www.archive.org/details/demo"))
	})
	e := evaluateLinked(t, linkedFixture(), lv)
	link := e.LibriVox
	if link == nil || link.Status != "linked" || link.ProjectID != "119" || link.Project == nil || *calls != 1 {
		t.Fatal(link)
	}
	p := link.Project
	if p.ArchiveItem != "demo" || p.URL == "" || len(p.Sections) != 2 || p.Sections[0].DeclaredSeconds != 506 || p.MetadataSHA256 == "" {
		t.Fatal(p)
	}
	if r := p.Sections[1].Readers; len(r) != 2 || r[0] != (Reader{ID: "209", Name: "Moira Fogarty"}) || r[1].ID != "964" {
		t.Fatal(r)
	}

	// A project that names another item is not linked and carries no readers.
	other, _ := librivoxProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, projectJSON("https://archive.org/details/another"))
	})
	if l := evaluateLinked(t, linkedFixture(), other).LibriVox; l.Status != "not_linked" || l.Project != nil {
		t.Fatal(l)
	}
	// A failing catalog leaves the Archive evaluation intact.
	down, _ := librivoxProvider(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if l := evaluateLinked(t, linkedFixture(), down).LibriVox; l.Status != "unavailable" || l.Project != nil || l.Detail == "" {
		t.Fatal(l)
	}
	// Without a configured client, nothing is fetched.
	if l := evaluateLinked(t, linkedFixture(), nil).LibriVox; l.Status != "not_checked" || l.ProjectID != "119" {
		t.Fatal(l)
	}
	// An item outside the LibriVox collection names no project.
	plain := librivoxFixture()
	plain["metadata"].(map[string]any)["call_number"] = "119"
	quiet, quietCalls := librivoxProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Error("fetched a project for a non-LibriVox item") })
	if l := evaluateLinked(t, plain, quiet).LibriVox; l != nil || *quietCalls != 0 {
		t.Fatal(l)
	}
}

func TestLibriVoxRecordsOutsideTheContractAreRefused(t *testing.T) {
	for name, body := range map[string]string{
		"another project":   `{"books":[{"id":"120","sections":[]}]}`,
		"no project":        `{"books":[]}`,
		"duplicate section": `{"books":[{"id":"119","sections":[{"section_number":"1","readers":[]},{"section_number":"1","readers":[]}]}]}`,
		"reader without id": `{"books":[{"id":"119","sections":[{"section_number":"1","readers":[{"display_name":"A"}]}]}]}`,
		"reader no name":    `{"books":[{"id":"119","sections":[{"section_number":"1","readers":[{"reader_id":"5","display_name":" "}]}]}]}`,
		"not json":          `[`,
	} {
		if _, err := parseLibriVox(json.RawMessage(body), "119"); err == nil {
			t.Fatal(name, "accepted")
		}
	}
	for _, id := range []string{"", "0", "012", "1a", "1234567890"} {
		if ValidLibriVoxID(id) {
			t.Fatal("accepted project ID", id)
		}
	}
	for raw, want := range map[string]string{
		"https://archive.org/details/demo":      "demo",
		"http://www.archive.org/details/demo/":  "demo",
		"https://evil.example/details/demo":     "",
		"https://archive.org/download/demo":     "",
		"https://archive.org/details/../x":      "",
		"https://user@archive.org/details/demo": "",
	} {
		if got := archiveDetailsItem(raw); got != want {
			t.Fatal(raw, got)
		}
	}
	// Bad playtime is dropped, not guessed; an untitled section stays.
	p, err := parseLibriVox(json.RawMessage(`{"books":[{"id":"119","url_librivox":"http://librivox.org/x","sections":[{"section_number":"1","playtime":"soon","readers":[]}]}]}`), "119")
	if err != nil || p.Sections[0].DeclaredSeconds != 0 || p.URL != "" {
		t.Fatal(p, err)
	}
}

// Readers are credited per track only when sections and tracks agree in
// count and declared length.
func TestLibriVoxReadersMatchTracksOnlyByCountAndLength(t *testing.T) {
	lv, _ := librivoxProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, projectJSON("https://archive.org/details/demo"))
	})
	e := evaluateLinked(t, linkedFixture(), lv)
	// The fixture declares 506 and 465 seconds (64 kbps) and 507.1 and 465.67
	// (VBR); the sections declare 506 and 466.
	for _, r := range e.Recordings {
		if r.ReaderEvidence != ReadersMatched || r.Tracks[0].Readers[0].Name != "Moira Fogarty" || len(r.Tracks[1].Readers) != 2 {
			t.Fatal(r.Format, r.ReaderEvidence, r.Tracks)
		}
	}
	far := linkedFixture()
	for _, f := range far["files"].([]any) {
		if m := f.(map[string]any); m["name"] == "b_03_64kb.mp3" {
			m["length"] = "12:00"
		}
	}
	for _, r := range evaluateLinked(t, far, lv).Recordings {
		want := ReadersMatched
		if r.Format == "64Kbps MP3" {
			want = ReadersNotMatched
		}
		if r.ReaderEvidence != want || (want == ReadersNotMatched && (r.Tracks[0].Readers != nil || r.Tracks[1].Readers != nil)) {
			t.Fatal(r.Format, r.ReaderEvidence, r.Tracks)
		}
	}
	one, _ := librivoxProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"books":[{"id":"119","url_iarchive":"https://archive.org/details/demo","sections":[{"section_number":"1","playtime":"506","readers":[{"reader_id":"209","display_name":"A"}]}]}]}`)
	})
	for _, r := range evaluateLinked(t, linkedFixture(), one).Recordings {
		if r.ReaderEvidence != ReadersNotMatched {
			t.Fatal("matched different counts", r.Format)
		}
	}
}

func TestLibriVoxSummaryDropsSectionsOnly(t *testing.T) {
	var none *LibriVoxLink
	if none.Summary() != nil {
		t.Fatal("summary of no link")
	}
	l := &LibriVoxLink{ProjectID: "119", Status: "linked", Project: &LibriVoxProject{ID: "119", ArchiveItem: "demo", Sections: []LibriVoxSection{{Number: 1}}}}
	s := l.Summary()
	if s.Status != "linked" || s.Project.ArchiveItem != "demo" || len(s.Project.Sections) != 0 || len(l.Project.Sections) != 1 {
		t.Fatal(s, l)
	}
}
