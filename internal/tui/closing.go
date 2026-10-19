package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/reader"
)

// Closed is where the reader closed, and the line that says so.
type Closed struct {
	At      reader.Position
	Summary string
}

// pagerAux is a read-only fact fetched beside the text: the book's declared
// contents, for naming where the reader is, or this library's saved
// bookmark for the asset. Neither is required, and neither writes anything.
type pagerAux struct {
	session int
	kind    string
	data    any
	err     error
}

// pagerFacts requests the declared contents of an EPUB and, in an
// initialized library, its bookmarks. Failures leave the unit label and say
// nothing about bookmarks.
func (m *model) pagerFacts() tea.Cmd {
	p := m.pager
	session, service := p.session, m.service
	catalog, root, asset := p.catalog, p.root, p.asset
	var cmds []tea.Cmd
	if p.representation != "pdf-text/1" {
		cmds = append(cmds, func() tea.Msg {
			data, err := service.Content(m.ctx, content.Request{SchemaVersion: 1, Catalog: catalog, Root: root, AssetID: asset, Representation: "epub-nav/1", UnitsOnly: true, MaxBytes: 4096})
			return pagerAux{session: session, kind: "contents", data: data, err: err}
		})
	}
	if catalog == root {
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithCancel(m.ctx)
			defer cancel()
			data, err := service.ListBookmarks(ctx, root)
			return pagerAux{session: session, kind: "bookmarks", data: data, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m *model) pagerAuxResult(msg pagerAux) {
	p := m.pager
	if p == nil || msg.session != p.session || msg.err != nil {
		return
	}
	switch v := msg.data.(type) {
	case content.Result:
		// One source of contents: the navigation document, else the NCX.
		for _, evidence := range []string{"nav-doc", "ncx"} {
			for _, doc := range v.Navigation {
				if doc.Evidence == evidence && doc.Problem == "" && len(doc.Entries) > 0 && p.contents == nil {
					p.contents = doc.Entries
				}
			}
		}
	case library.BookmarkState:
		for _, b := range v.Bookmarks {
			if b.AssetID == p.asset {
				saved := b
				p.saved = &saved
			}
		}
	}
}

// placeName names a position: the deepest declared contents entry that
// starts at or before it, else the unit label.
func (m *model) placeName(at reader.Position) string {
	p := m.pager
	name := ""
	for _, e := range p.contents {
		if e.Status != "resolved" || e.Unit == nil || e.Offset == nil || e.Label == "" {
			continue
		}
		if (reader.Position{Unit: *e.Unit, Offset: *e.Offset}).Less(at) || (reader.Position{Unit: *e.Unit, Offset: *e.Offset}) == at {
			name = e.Label
		}
	}
	if name != "" {
		return name
	}
	return p.doc.Label(at.Unit)
}

// through is how far into the book's text a position is, rounded down, or
// -1 when the units do not state their text sizes.
func (m *model) through(at reader.Position) int {
	total, before := 0, 0
	for i, u := range m.pager.units {
		total += u.TextBytes
		if i < at.Unit {
			before += u.TextBytes
		}
	}
	if total == 0 {
		return -1
	}
	return min(100, (before+at.Offset)*100/total)
}

// closingLine is the reader's receipt: where it closed, how far through, and
// that nothing was recorded. It names a saved bookmark only when one exists
// for this book at another place, since closing never moves it.
func (m *model) closingLine(at reader.Position) string {
	line := "Closed at " + m.placeName(at)
	if pct := m.through(at); pct >= 0 {
		line += fmt.Sprintf(", %d%% through", pct)
	}
	line += ". Nothing about this reading was recorded."
	if s := m.pager.saved; s != nil && (s.Unit != at.Unit || s.Offset != at.Offset) {
		line += " Closing does not save a bookmark; yours is still at " + m.placeName(reader.Position{Unit: s.Unit, Offset: s.Offset}) + "."
	}
	return line
}
