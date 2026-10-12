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
	// mpeg-audio/1 for an MP3, pdf-text/1 for a PDF, otherwise epub-structure/1.
	representation          string
	next                    string
	boundCatalog, boundRoot string
	last                    content.Request
	hasLast                 bool
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

// readSelected opens the reader at the selected unit of a units listing for
// the current inputs, at the unit of the last range read, or at the first. EPUBs read as
// epub-text/2, which shares the structured representation's unit indexes and
// also reads books that representation refuses; PDFs read as pdf-text/1.
func (m *model) readSelected(speed bool) tea.Cmd {
	if m.busy {
		return nil
	}
	switch {
	case m.reader.asset == "" || m.input.Value() == "" || m.secondary.Value() == "":
		m.status = "Select an asset in Holdings with r and enter its explicit root."
	case m.reader.representation == "mpeg-audio/1":
		m.status = "Audio has no text to page. Enter retrieves frame ranges."
	default:
		representation, unit := "epub-text/2", 0
		if m.reader.representation == "pdf-text/1" {
			representation = "pdf-text/1"
		}
		bound := m.reader.boundCatalog == m.input.Value() && m.reader.boundRoot == m.secondary.Value()
		last := m.reader.last
		switch {
		case bound && len(m.entries) > 0:
			unit = m.selected
		case bound && m.reader.hasLast && !last.UnitsOnly && last.Cursor == "":
			// After a range was read, the unit it was read from.
			unit = last.Unit
		}
		return m.openPager(m.input.Value(), m.secondary.Value(), m.reader.asset, representation, reader.Position{Unit: unit}, speed, reader.DefaultRate)
	}
	m.resize()
	return nil
}
