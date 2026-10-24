package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/present"
	"github.com/blisspixel/nemalo/internal/reader"
)

const maxContentsChoices = 2048

type contentsChoice struct {
	label, source string
	entry         content.NavEntry
	page          bool
	unit          int
}
type readerContents struct {
	request  int
	cancel   context.CancelFunc
	fetching bool
	choices  []contentsChoice
	selected int
	notice   string
}
type contentsResult struct {
	session, request int
	data             content.Result
	err              error
}

// The chooser is transient presentation of canonical source declarations. It
// neither resolves links itself nor changes bookmarks or reading state on disk.
func (m *model) openContents() tea.Cmd {
	p := m.pager
	if p.doc == nil {
		p.notice = "Wait for the source text before opening contents."
		return nil
	}
	p.playing, p.waiting = false, false
	p.gen = m.nextSeq()
	n := &readerContents{request: m.nextSeq(), notice: "Retrieving source-declared contents."}
	p.chooser = n
	if p.representation == "pdf-text/1" {
		for i, u := range p.units {
			if len(n.choices) == maxContentsChoices {
				n.notice = "Display limited to the first 2048 physical pages; [ ] still selects adjacent pages."
				return tea.ClearScreen
			}
			label := fmt.Sprintf("Physical page %d", i+1)
			if u.PageLabel != "" {
				label += "; declared label " + present.Line(u.PageLabel)
			}
			n.choices = append(n.choices, contentsChoice{label: label, page: true, unit: i})
		}
		n.notice = "Physical pages are source units, not inferred chapters. Enter opens a paused page."
		return tea.ClearScreen
	}
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	n.cancel, n.fetching = cancel, true
	session, request, service := p.session, n.request, m.service
	req := content.Request{SchemaVersion: 1, Catalog: p.catalog, Root: p.root, AssetID: p.asset, Representation: "epub-nav/1", UnitsOnly: true, MaxBytes: 4096}
	return tea.Batch(tea.ClearScreen, func() tea.Msg {
		defer cancel()
		data, err := service.Content(ctx, req)
		return contentsResult{session, request, data, err}
	})
}

func (m *model) contentsResult(msg contentsResult) {
	p := m.pager
	if p == nil || p.session != msg.session || p.chooser == nil || p.chooser.request != msg.request || !p.chooser.fetching {
		return
	}
	n := p.chooser
	n.fetching = false
	if n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
	if msg.err != nil {
		n.notice = "Contents unavailable: " + present.Error(msg.err)
		return
	}
	n.notice = "Choose a declared entry. Source labels distinguish NAV and NCX; no source is chosen automatically."
	for _, doc := range msg.data.Navigation {
		if len(n.choices) == maxContentsChoices {
			n.notice = "Display limited to the first 2048 declared entries; later entries are not shown."
			return
		}
		if doc.Problem != "" {
			n.choices = append(n.choices, contentsChoice{source: doc.Evidence, label: doc.Problem, entry: content.NavEntry{Status: "unavailable"}})
		} else {
			for _, entry := range doc.Entries {
				if len(n.choices) == maxContentsChoices {
					n.notice = "Display limited to the first 2048 declared entries; later entries are not shown."
					return
				}
				n.choices = append(n.choices, contentsChoice{label: entry.Label, source: doc.Evidence, entry: entry})
			}
		}
	}
	if len(n.choices) == 0 {
		n.notice = "No source-declared contents. [ ] selects source units after closing this list."
	}
}

func (m *model) closeContents() {
	if n := m.pager.chooser; n != nil && n.cancel != nil {
		n.cancel()
	}
	m.pager.chooser = nil
}

func (m *model) contentsKey(key string) tea.Cmd {
	p, n := m.pager, m.pager.chooser
	switch key {
	case "c", "esc":
		m.closeContents()
		return tea.ClearScreen
	case "j", "down":
		n.selected = min(max(0, len(n.choices)-1), n.selected+1)
	case "k", "up":
		n.selected = max(0, n.selected-1)
	case "space", "pgdown":
		n.selected = min(max(0, len(n.choices)-1), n.selected+m.pagerBody())
	case "b", "pgup":
		n.selected = max(0, n.selected-m.pagerBody())
	case "g", "home":
		n.selected = 0
	case "G", "end":
		n.selected = max(0, len(n.choices)-1)
	case "enter":
		if n.fetching || len(n.choices) == 0 {
			return nil
		}
		choice := n.choices[n.selected]
		if choice.page {
			m.closeContents()
			return tea.Batch(m.pagerJump(choice.unit), tea.ClearScreen)
		}
		e := choice.entry
		if e.Status != "resolved" || e.Unit == nil || e.Offset == nil || choice.source != "nav-doc" && choice.source != "ncx" {
			n.notice = "This declared entry has no resolved source location; nothing was opened."
			return nil
		}
		catalog, root, asset, only, speed, rate := p.catalog, p.root, p.asset, p.only, p.speed, p.rate
		m.closeContents()
		if p.cancel != nil {
			p.cancel()
		}
		cmd := m.openChapter(catalog, root, asset, choice.source, e.Order, reader.Position{Unit: *e.Unit, Offset: *e.Offset}, speed)
		m.pager.only, m.pager.rate = only, rate
		return tea.Batch(cmd, tea.ClearScreen)
	}
	return nil
}

func (m *model) contentsBody(margin string) []string {
	n := m.pager.chooser
	lines := []string{margin + "Contents and source pages"}
	body := max(1, m.pagerBody()-2)
	top := n.selected / body * body
	for i := top; i < min(len(n.choices), top+body); i++ {
		c := n.choices[i]
		label := present.Line(c.label)
		if !c.page {
			label = "[" + present.Line(c.source) + "] " + strings.Repeat("  ", min(8, max(0, c.entry.Level-1))) + label
			label += " (entry " + strconv.Itoa(c.entry.Order) + ", " + present.Line(c.entry.Status) + ")"
		}
		mark := "  "
		if i == n.selected {
			mark = "> "
		}
		lines = append(lines, margin+mark+label)
	}
	return lines
}

func contentsHelp() []string {
	return []string{
		"Contents keys",
		"j/k, Down/Up  choose an entry",
		"Space, PageDown  next page of choices",
		"b, PageUp        previous page of choices",
		"g/Home, G/End    first or last choice",
		"Enter           open the selected source range, paused",
		"c               after hiding help, return to the passage",
		"q, Escape       close the reader; nothing is recorded",
		"?               return to the choices",
	}
}
