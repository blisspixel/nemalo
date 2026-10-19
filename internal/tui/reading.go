package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/reader"
)

// Session delivery references are navigation aids, never consumption records.
type readerState struct {
	asset string
	// representation is chosen when an asset is staged from holdings:
	// mpeg-audio/1 for an MP3, mp4-audio/1 for an M4B or M4A, pdf-text/1 for
	// a PDF, otherwise epub-structure/1.
	representation          string
	next                    string
	boundCatalog, boundRoot string
	last                    content.Request
	hasLast                 bool
	// mark* is the last delivered source location in this session. It is not
	// a bookmark until the reader saves it. A failed or cancelled read leaves
	// it as it was.
	markOK                                   bool
	markUnit, markOffset                     int
	markRepresentation, markRepresentationID string
	markCursor, markAsset                    string
	// markCatalog and markRoot are the scope the location was read in.
	markCatalog, markRoot string
	// markNav is set for a declared-entry read: the entry is markNavEvidence
	// and markNavOrder, and markCursor continues that entry at the location.
	markNav         bool
	markNavEvidence string
	markNavOrder    int
}

// remember keeps the location a successful read delivered. A later explicit
// save can use it. Listing units does not count as a location.
func (m *model) remember(value content.Result) {
	if value.Locator == nil || (value.Status != "partial_range" && value.Status != "complete_range") {
		return
	}
	m.reader.markOK = true
	m.reader.markUnit = value.Locator.Unit
	m.reader.markOffset = value.Locator.Start
	m.reader.markRepresentation = value.Representation
	m.reader.markRepresentationID = value.Locator.RepresentationID
	m.reader.markAsset = value.AssetID
	m.reader.markCatalog, m.reader.markRoot = m.reader.last.Catalog, m.reader.last.Root
	m.reader.markNav, m.reader.markNavEvidence, m.reader.markNavOrder = false, "", 0
	if value.Representation == "epub-nav/1" && len(value.Navigation) == 1 && len(value.Navigation[0].Entries) == 1 {
		m.reader.markNav = true
		m.reader.markNavEvidence, m.reader.markNavOrder = value.Navigation[0].Evidence, value.Navigation[0].Entries[0].Order
	}
	m.reader.markCursor = ""
	if m.reader.last.Cursor != "" {
		m.reader.markCursor = m.reader.last.Cursor
		return
	}
	if value.Representation == "epub-nav/1" && m.reader.last.Navigation != "" {
		token, err := content.NavigationCursor(value.AssetID, value.Locator.RepresentationID, m.reader.last.Navigation, m.reader.last.Unit, value.Locator.Unit, value.Locator.Start)
		if err == nil {
			m.reader.markCursor = token
		}
	}
}

func (m *model) startRead(units bool, unit int, cursor string, repeat bool) tea.Cmd {
	if m.busy {
		return nil
	}
	if m.reader.asset == "" || m.input.Value() == "" || m.secondary.Value() == "" {
		m.status = "Select an asset in Holdings with r and enter its explicit root."
		m.resize()
		return nil
	}
	repeatScopeChanged := !m.reader.hasLast || m.reader.last.Catalog != m.input.Value() || m.reader.last.Root != m.secondary.Value()
	nextScopeChanged := m.reader.boundCatalog != m.input.Value() || m.reader.boundRoot != m.secondary.Value()
	if (repeat && repeatScopeChanged) || (cursor != "" && nextScopeChanged) {
		m.status = "Inputs changed. Enter lists units for this root before continuing."
		m.resize()
		return nil
	}
	req := content.Request{
		SchemaVersion: 1, Catalog: m.input.Value(), Root: m.secondary.Value(),
		AssetID: m.reader.asset, Representation: "epub-structure/1",
		UnitsOnly: units, Unit: unit, Cursor: cursor, MaxBytes: 4096,
	}
	if m.reader.representation == "mpeg-audio/1" {
		// Whole frames plus decoder preroll need more than a text page.
		req.Representation, req.MaxBytes = "mpeg-audio/1", 65536
	}
	if m.reader.representation == "pdf-text/1" {
		req.Representation = "pdf-text/1"
	}
	if m.reader.representation == "mp4-audio/1" {
		if !units && !repeat {
			m.status = "An M4B or M4A lists its chapters only. In Holdings, o opens a verified copy in a player."
			m.resize()
			return nil
		}
		req.Representation = "mp4-audio/1"
	}
	if !units && cursor == "" && !repeat && len(m.entries) > 0 && m.entries[m.selected].nav {
		req.Representation = "epub-nav/1"
		req.Navigation = m.entries[m.selected].navEvidence
		req.Unit = m.entries[m.selected].navOrder
	}
	if cursor != "" && m.reader.hasLast && m.reader.last.Representation == "epub-nav/1" {
		req.Representation = "epub-nav/1"
		req.Navigation = ""
		req.Unit = 0
	}
	if !units && cursor == "" && !repeat && len(m.entries) > 0 && m.entries[m.selected].page {
		e := m.entries[m.selected]
		if e.pageRef == "" {
			m.status = "This page label has no resolved location in the book."
			m.resize()
			return nil
		}
		// The marker's own reference reads from it; it is bound to the page list.
		req.Representation, req.Cursor, req.Unit = "", e.pageRef, 0
	}
	if cursor != "" && m.reader.hasLast && (m.reader.last.Representation == "epub-page-list/1" || m.reader.last.Representation == "") {
		// A read from a page label continues in epub-text/2; the cursor names it.
		req.Representation = ""
	}
	if repeat {
		req = m.reader.last
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.id++
	id, service := m.id, m.service
	m.reader.last, m.reader.hasLast = req, true
	m.busy = true
	m.status = "Retrieving source bytes. Escape cancels; no progress is recorded."
	m.setReport("")
	m.populate(nil)
	m.resize()
	return func() tea.Msg {
		data, err := service.Content(ctx, req)
		return result{id: id, data: data, err: err}
	}
}

// listPages shows the book's declared page labels. It does not read text
// and does not record a position.
func (m *model) listPages() tea.Cmd {
	if m.busy {
		return nil
	}
	if m.reader.asset == "" || m.input.Value() == "" || m.secondary.Value() == "" {
		m.status = "Select an asset in Holdings with r and enter its explicit root."
		m.resize()
		return nil
	}
	if m.reader.representation == "mpeg-audio/1" || m.reader.representation == "mp4-audio/1" || m.reader.representation == "pdf-text/1" {
		m.status = "Declared page labels are an EPUB page list. A PDF's printed labels are in its units (u)."
		m.resize()
		return nil
	}
	req := content.Request{
		SchemaVersion: 1, Catalog: m.input.Value(), Root: m.secondary.Value(),
		AssetID: m.reader.asset, Representation: "epub-page-list/1", UnitsOnly: true, MaxBytes: 4096,
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.id++
	id, service := m.id, m.service
	m.reader.last, m.reader.hasLast = req, true
	m.busy = true
	m.status = "Reading the declared page labels. Nothing is recorded."
	m.setReport("")
	m.populate(nil)
	m.resize()
	return func() tea.Msg {
		data, err := service.Content(ctx, req)
		return result{id: id, data: data, err: err}
	}
}

// listNavigation shows the book's declared contents. It does not read text
// and does not record a position.
func (m *model) listNavigation() tea.Cmd {
	if m.busy {
		return nil
	}
	if m.reader.asset == "" || m.input.Value() == "" || m.secondary.Value() == "" {
		m.status = "Select an asset in Holdings with r and enter its explicit root."
		m.resize()
		return nil
	}
	if m.reader.representation == "mpeg-audio/1" || m.reader.representation == "mp4-audio/1" || m.reader.representation == "pdf-text/1" {
		m.status = "Declared navigation is an EPUB contents list."
		m.resize()
		return nil
	}
	req := content.Request{
		SchemaVersion: 1, Catalog: m.input.Value(), Root: m.secondary.Value(),
		AssetID: m.reader.asset, Representation: "epub-nav/1", UnitsOnly: true, MaxBytes: 4096,
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.id++
	id, service := m.id, m.service
	m.reader.last, m.reader.hasLast = req, true
	m.busy = true
	m.status = "Reading the declared contents. Nothing is recorded."
	m.setReport("")
	m.populate(nil)
	m.resize()
	return func() tea.Msg {
		data, err := service.Content(ctx, req)
		return result{id: id, data: data, err: err}
	}
}

// readSelected opens the reader at the selected entry of a units or contents
// listing for the current inputs; else where the last successful excerpt of
// this asset and scope began; else at the first unit. EPUBs read as
// epub-text/2, which shares the structured and navigation representations'
// text coordinates and also reads books those refuse; PDFs read as pdf-text/1.
// A declared-entry excerpt reopens that entry, so pages stop at its end.
func (m *model) readSelected(speed bool) tea.Cmd {
	if m.busy {
		return nil
	}
	switch {
	case m.reader.asset == "" || m.input.Value() == "" || m.secondary.Value() == "":
		m.status = "Select an asset in Holdings with r and enter its explicit root."
	case m.reader.representation == "mpeg-audio/1":
		m.status = "Audio has no text to page. Enter retrieves frame ranges."
	case m.reader.representation == "mp4-audio/1":
		m.status = "Audio has no text to page. u lists its chapters."
	default:
		representation := "epub-text/2"
		if m.reader.representation == "pdf-text/1" {
			representation = "pdf-text/1"
		}
		catalog, root, asset := m.input.Value(), m.secondary.Value(), m.reader.asset
		bound := m.reader.boundCatalog == catalog && m.reader.boundRoot == root
		if bound && len(m.entries) > 0 && m.entries[m.selected].page {
			e := m.entries[m.selected]
			if e.pageRef == "" {
				m.status = "This page label has no resolved location in the book."
				break
			}
			return m.openPager(catalog, root, asset, representation, reader.Position{Unit: e.pageUnit, Offset: e.pageOffset}, speed, reader.DefaultRate)
		}
		if bound && len(m.entries) > 0 && m.entries[m.selected].nav {
			e := m.entries[m.selected]
			if !e.navResolved {
				m.status = "This declared entry has no resolved location in the book."
				break
			}
			return m.openChapter(catalog, root, asset, e.navEvidence, e.navOrder, reader.Position{Unit: e.navUnit, Offset: e.navOffset}, speed)
		}
		// A listing on screen is a deliberate choice; it comes before the
		// last excerpt.
		if bound && len(m.entries) > 0 {
			return m.openPager(catalog, root, asset, representation, reader.Position{Unit: m.selected}, speed, reader.DefaultRate)
		}
		at, ok := m.handoff(representation)
		switch {
		case ok && m.reader.markNav && m.reader.markCursor == "":
			m.status = "That declared entry cannot be reopened at the last passage. Choose it again in the contents list (c)."
		case ok && m.reader.markNav:
			cmd := m.openChapter(catalog, root, asset, m.reader.markNavEvidence, m.reader.markNavOrder, at, speed)
			// The entry continues at the excerpt's start, in its own representation.
			m.pager.chapterCursor = m.reader.markCursor
			return cmd
		case ok:
			return m.openPager(catalog, root, asset, representation, at, speed, reader.DefaultRate)
		default:
			return m.openPager(catalog, root, asset, representation, reader.Position{}, speed, reader.DefaultRate)
		}
	}
	m.resize()
	return nil
}

// handoff is where the last successful excerpt began, when it was read from
// this asset, catalog, and root in a representation whose text coordinates
// the reader's representation shares. Only the unit and byte move: no token
// or representation identity is reused, and the reader's own read reports a
// changed or missing source instead of mapping it elsewhere. The excerpt's
// continuation names text not yet delivered, so it is never the start.
func (m *model) handoff(representation string) (reader.Position, bool) {
	r := m.reader
	if !r.markOK || r.markAsset != r.asset || r.markCatalog != m.input.Value() || r.markRoot != m.secondary.Value() {
		return reader.Position{}, false
	}
	shared := r.markRepresentation == representation
	if representation == "epub-text/2" {
		shared = r.markRepresentation == "epub-text/2" || r.markRepresentation == "epub-structure/1" || r.markRepresentation == "epub-nav/1" || r.markRepresentation == "epub-page-list/1"
	}
	if !shared {
		return reader.Position{}, false
	}
	return reader.Position{Unit: r.markUnit, Offset: r.markOffset}, true
}
