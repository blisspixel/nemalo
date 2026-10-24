package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/present"
)

// readAux owns only its report and captured request. Ordinary read navigation,
// form drafts and explicit bookmark state are never replaced by these replies.
type readAux struct {
	citation                  bool
	request                   content.Request
	reference, report, status string
	viewport                  viewport.Model
}

type readAuxReply struct {
	screen *readAux
	id     int
	data   any
	err    error
}

func (m *model) openReadAux(citation bool) tea.Cmd {
	if m.mode != "read" || m.busy || m.readAux != nil || m.pager != nil {
		return nil
	}
	if m.reader.asset == "" || m.input.Value() == "" || m.secondary.Value() == "" {
		m.status = "Select an asset and explicit library/root before requesting citation or evidence."
		m.resize()
		return nil
	}
	req := content.Request{Catalog: m.input.Value(), Root: m.secondary.Value(), AssetID: m.reader.asset}
	if !citation {
		last := m.reader.delivered
		if last == nil || last.Catalog != req.Catalog || last.Root != req.Root || last.AssetID != req.AssetID {
			m.status = "Read a successful excerpt for this asset, catalog and root before F4 evidence. A units listing is not an excerpt."
			m.resize()
			return nil
		}
		req = *last
	}
	v := viewport.New()
	v.SoftWrap = false // hardWrap preserves every cell, including long references.
	m.readAux = &readAux{citation: citation, request: req, viewport: v}
	return m.requestReadAux(false)
}

func (m *model) requestReadAux(replay bool) tea.Cmd {
	screen := m.readAux
	if screen == nil || m.busy || replay && (screen.citation || screen.reference == "") {
		return nil
	}
	req := screen.request
	req.Evidence, req.EvidenceReference = !screen.citation, ""
	if replay {
		req.Evidence, req.EvidenceReference = false, screen.reference
	}
	// A failed/cancelled new request cannot leave an old quotation or token.
	screen.reference, screen.report = "", ""
	screen.status = "Reading exact source evidence. Esc cancels. No progress is recorded."
	deadline := 5 * time.Minute
	if screen.citation {
		deadline = 15 * time.Second
		screen.status = "Reading recorded citation metadata. Stored content is not opened. Esc cancels."
	} else if replay {
		screen.status = "Replaying the same bounded selection and reference. Esc cancels."
	}
	ctx, cancel := context.WithTimeout(m.ctx, deadline)
	m.cancel = cancel
	m.id++
	id, service := m.id, m.service
	m.busy = true
	m.layoutReadAux()
	return func() tea.Msg {
		if screen.citation {
			data, err := service.Citation(ctx, req.Catalog, req.AssetID)
			return readAuxReply{screen, id, data, err}
		}
		data, err := service.Content(ctx, req)
		return readAuxReply{screen, id, data, err}
	}
}

func (m *model) acceptReadAux(reply readAuxReply) {
	screen := m.readAux
	if screen == nil || screen != reply.screen || reply.id != m.id {
		return
	}
	if m.mode != "read" || screen.request.Catalog != m.input.Value() || screen.request.Root != m.secondary.Value() || screen.request.AssetID != m.reader.asset {
		m.closeReadAux()
		return
	}
	m.busy = false
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if reply.err != nil {
		screen.status = "Unavailable. The response payload and replay reference were discarded."
		screen.report = "Operation error: " + present.Error(reply.err)
	} else {
		switch data := reply.data.(type) {
		case app.CitationResult:
			screen.report = present.Citation(data)
			screen.status = "Recorded declarations with unknown fields and limits. No fresh content, rights or safety check."
		case content.Result:
			screen.report = present.Content(data)
			screen.status = "This returned range only. Reference comparison is not interpretation or permission."
			if data.Evidence != nil {
				screen.reference = data.Evidence.Reference
			}
		}
	}
	screen.viewport.GotoTop()
	m.layoutReadAux()
}

func (m *model) closeReadAux() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.id++
	m.busy = false
	m.readAux = nil
}

func (m *model) readAuxKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.Keystroke() {
	case "ctrl+c":
		m.closeReadAux()
		return tea.Quit
	case "esc":
		m.closeReadAux()
		return nil
	case "f3", "r": // r needs no function key (decision 0094).
		return m.requestReadAux(true)
	case "home":
		m.readAux.viewport.GotoTop()
	case "end":
		m.readAux.viewport.GotoBottom()
	case "pgup", "pgdown", "up", "down":
		// Pass only the physical control into the viewport, never associated text.
		control := msg
		control.Text = ""
		m.readAux.viewport, _ = m.readAux.viewport.Update(control)
	}
	return nil
}

func (m *model) layoutReadAux() {
	if s := m.readAux; s != nil {
		s.viewport.SetWidth(max(1, m.width-2))
		s.viewport.SetHeight(max(1, m.height-5))
		s.viewport.SetContent(hardWrap(s.report, max(1, m.width-2)))
	}
}

func (m *model) readAuxView() string {
	s := m.readAux
	title := "Source-bound evidence"
	if s.citation {
		title = "Recorded citation metadata"
	}
	footer := "PgUp/Dn scroll  Esc back  ^C quit"
	if !s.citation && s.reference != "" && !m.busy {
		footer = "F3/r replay  PgUp/Dn  Esc back  ^C quit"
	}
	lines := []string{m.paint(clip("Nemalo | "+title, m.width-2), accent, true), clip(s.status, m.width-2), strings.Repeat("-", m.width-2), s.viewport.View(), clip(footer, m.width-2)}
	// The viewport occupies height-5 rows; one final empty row keeps full-frame
	// repaints within the terminal, just like the ordinary reader.
	return strings.Join(lines, "\n")
}
