package tui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
)

func homeDoctor(available bool) *app.Doctor {
	return &app.Doctor{Capabilities: []app.Capability{{Name: "clamscan", Available: available, Recommended: true}}}
}

// Without a library Home welcomes and each start lands on its section,
// with no request and no write.
func TestHomeFirstRunWelcomesWithoutReading(t *testing.T) {
	calls := 0
	m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{"openlibrary": countingSearcher{&calls}}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd := m.configure(Options{Doctor: homeDoctor(false)}); cmd != nil {
		t.Fatal("first run started work")
	}
	if m.mode != "home" || len(m.entries) != 3 || !strings.Contains(m.report, "No library is configured") || !strings.Contains(m.report, "clamscan not found") {
		t.Fatalf("welcome %q %v", m.report, m.entries)
	}
	view := m.View().Content
	for _, want := range []string{"[Home]", "Find a public-domain book", "nemalo mcp serve", "--json"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
	for i, want := range []string{"search", "inspect", "state"} {
		m.switchMode("home")
		m.Update(key(tea.KeyF6))
		m.selectEntry(i)
		m.Update(key(tea.KeyEnter))
		if m.mode != want || m.busy {
			t.Fatalf("choice %d reached %s busy=%t", i, m.mode, m.busy)
		}
	}
	if calls != 0 {
		t.Fatal("Home made a provider request")
	}
}

// A configured library prefills fields and is read once, offline, without
// changing a byte; Library then lists holdings without typing a path.
func TestHomeConfiguredLibraryOverviewReadsWithoutWriting(t *testing.T) {
	root, jobs, _ := terminalJobFixture(t)
	held := holdTUIBook(t, context.Background(), root, "book.epub", plainEPUB(t, "Overview word."))
	before := terminalJobTree(t, root)
	m := newModel(context.Background(), app.Service{JobsDirectory: jobs})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	cmd := m.configure(Options{Library: root, Doctor: homeDoctor(true)})
	if cmd == nil || !m.busy || m.mode != "home" || m.input.Value() != root {
		t.Fatal("configured library was not read", m.mode, m.status)
	}
	m.Update(cmd())
	for _, want := range []string{"Status     initialized", "Holdings   0 checked, 1 in review", "clamscan available", "Jobs       1 recorded, 1 recorded as unfinished", "Bookmarks  not enabled", "nothing was written"} {
		if !strings.Contains(m.report, want) {
			t.Fatalf("missing %q in\n%s", want, m.report)
		}
	}
	if !reflect.DeepEqual(before, terminalJobTree(t, root)) {
		t.Fatal("the overview changed the library")
	}
	for step, want := range map[string]string{"holdings": root, "read": root, "state": root, "jobs": root, "audit": root} {
		if m.drafts[step].input != want {
			t.Fatalf("%s not prefilled: %q", step, m.drafts[step].input)
		}
	}
	if m.drafts["read"].second != root || m.drafts["collections"].second != root {
		t.Fatal("read root or collection library not prefilled")
	}
	m.Update(key(tea.KeyF6))
	for i, e := range m.entries {
		if e.title == "Library" {
			m.selectEntry(i)
		}
	}
	_, cmd = m.Update(key(tea.KeyEnter))
	if m.mode != "holdings" || cmd == nil || !m.busy {
		t.Fatal("Library did not list holdings", m.mode, m.status)
	}
	run(m, cmd)
	if len(m.entries) != 1 || m.entries[0].identity != held.AssetID {
		t.Fatal("holdings", m.entries, m.status)
	}
}

// Unavailable and uninitialized folders get a clear state and nothing is
// created; explicit bookmarks are the only Continue entries.
func TestHomeStatesAndExplicitBookmarks(t *testing.T) {
	ctx := context.Background()
	m := newModel(ctx, app.Service{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	missing := filepath.Join(t.TempDir(), "missing")
	m.configure(Options{})
	m.input.SetValue(missing)
	run(m, m.start(true))
	if !strings.Contains(m.report, "unavailable") || m.outcome != outcomeAttention {
		t.Fatal(m.report)
	}
	fresh := t.TempDir()
	if _, err := library.Initialize(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue(fresh)
	run(m, m.start(true))
	if !strings.Contains(m.report, "Bookmarks  not enabled") || strings.Contains(m.report, "not read") {
		t.Fatal(m.report)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("overview created the folder")
	}
	plain := t.TempDir()
	m.input.SetValue(plain)
	run(m, m.start(true))
	if !strings.Contains(m.report, "not initialized") {
		t.Fatal(m.report)
	}
	if _, err := os.Stat(filepath.Join(plain, ".nemalo")); !os.IsNotExist(err) {
		t.Fatal("overview created control state")
	}
	root := t.TempDir()
	if _, err := library.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	held := holdTUIBook(t, ctx, root, "book.epub", plainEPUB(t, "Bookmark word."))
	if _, err := library.EnableBookmarks(ctx, root); err != nil {
		t.Fatal(err)
	}
	if _, err := library.SaveBookmark(ctx, root, library.BookmarkLocation{AssetID: held.AssetID, Representation: "epub-text/2", RepresentationID: "sha256:" + strings.Repeat("a", 64), Unit: 0, Offset: 3}); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue(root)
	run(m, m.start(true))
	if !strings.Contains(m.report, "Bookmarks  1 saved; only explicit bookmarks are kept") || m.entries[0].bookmark == nil || !strings.HasPrefix(m.entries[0].title, "Continue: ") {
		t.Fatal(m.report, m.entries)
	}
	m.Update(key(tea.KeyF6))
	_, cmd := m.Update(key(tea.KeyEnter))
	if m.mode != "read" || m.reader.asset != held.AssetID || m.input.Value() != root || m.secondary.Value() != root || cmd == nil {
		t.Fatal("bookmark did not open in Read", m.mode, m.status)
	}
	view := m.View().Content
	if strings.Contains(view, "\x1b") && !m.color {
		t.Fatal("styles without colour")
	}
}

type countingSearcher struct{ calls *int }

func (c countingSearcher) Search(context.Context, string, int, int) (discovery.Page, error) {
	*c.calls++
	return discovery.Page{}, nil
}
