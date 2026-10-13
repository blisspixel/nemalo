package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/content"
)

// saveBookmark records the on-screen source location, or the last delivered
// range when the pager is closed. It does not enable bookmarks and does not
// run on quit.
func (m *model) saveBookmark() tea.Cmd {
	if m.busy {
		return nil
	}
	dir, asset, representation, representationID, cursor := m.input.Value(), m.reader.asset, m.reader.markRepresentation, m.reader.markRepresentationID, m.reader.markCursor
	unit, offset, ok := m.reader.markUnit, m.reader.markOffset, m.reader.markOK && m.reader.markAsset == asset
	if m.pager != nil {
		dir, asset = m.pager.catalog, m.pager.asset
		representation, representationID = m.pager.representation, m.pager.representationID
		cursor = ""
		if at, posOK := m.pagerPosition(); posOK && representationID != "" {
			unit, offset, ok = at.Unit, at.Offset, true
		} else {
			ok = false
		}
		if m.pager.chapter && ok {
			token, err := content.NavigationCursor(asset, representationID, m.pager.navEvidence, m.pager.navOrder, unit, offset)
			if err != nil {
				m.bookmarkStatus("This declared entry has no exact location to save.")
				return nil
			}
			cursor = token
		}
		if m.pager.catalog != m.pager.root {
			m.bookmarkStatus("Bookmarks belong to an initialized library. Nothing was saved.")
			return nil
		}
	} else if m.input.Value() == "" || m.input.Value() != m.secondary.Value() {
		m.bookmarkStatus("Bookmarks belong to an initialized library. Use that directory as both catalog and root.")
		return nil
	}
	if !ok || asset == "" || representation == "" || representationID == "" {
		m.bookmarkStatus("Retrieve a source location, then press m to save it. Nothing was saved.")
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.id++
	id, service := m.id, m.service
	request := app.BookmarkSave{
		Library: dir, AssetID: asset, Representation: representation, RepresentationID: representationID,
		Unit: unit, Offset: offset, Cursor: cursor,
	}
	m.busy = true
	m.bookmarkStatus("Saving this location. Closing does not save a bookmark.")
	return func() tea.Msg {
		st, err := service.LibraryStatus(ctx, dir)
		if err != nil || st.Status != "initialized" {
			return result{id: id, err: errors.New("bookmarks belong to an initialized library; nothing was saved")}
		}
		mark, err := service.SaveBookmark(ctx, request)
		if err != nil {
			return result{id: id, err: err}
		}
		return result{id: id, data: mark}
	}
}

// restoreBookmark opens the saved location for the current asset. It does not
// run when a book is merely selected or when the reader closes.
func (m *model) restoreBookmark() tea.Cmd {
	if m.busy {
		return nil
	}
	dir, asset := m.input.Value(), m.reader.asset
	if m.pager != nil {
		dir, asset = m.pager.catalog, m.pager.asset
		if m.pager.catalog != m.pager.root {
			m.bookmarkStatus("Bookmarks belong to an initialized library. Nothing was opened.")
			return nil
		}
		_ = m.closePager()
	} else if m.input.Value() == "" || m.input.Value() != m.secondary.Value() {
		m.bookmarkStatus("Bookmarks belong to an initialized library. Use that directory as both catalog and root.")
		return nil
	}
	if asset == "" {
		m.bookmarkStatus("Select an asset before restoring a bookmark.")
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.id++
	id, service := m.id, m.service
	m.busy = true
	m.bookmarkStatus("Restoring the saved location. Nothing else is recorded.")
	return func() tea.Msg {
		data, err := service.ResumeBookmark(ctx, dir, asset)
		return result{id: id, data: data, err: err}
	}
}

// bookmarkStatus is visible in the pager, which replaces the browser status line.
func (m *model) bookmarkStatus(status string) {
	m.status = status
	if m.pager != nil {
		m.pager.notice = status
	}
	m.resize()
}
