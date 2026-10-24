package tui

import (
	"context"
	"fmt"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/present"
	"github.com/blisspixel/nemalo/internal/reader"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

const maxFindBytes = 1000
const maxFindMatches = 100

type readerFind struct {
	input             textinput.Model
	representation    string
	query             string
	matches           []content.Match
	selected, request int
	unsupported       int
	editing, fetching bool
	cancel            context.CancelFunc
	notice            string
}
type findResult struct {
	session, request int
	data             content.Result
	err              error
}
type findInputResult struct {
	session, request int
	message          tea.Msg
}

func findInputCommand(session, request int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			wrapped := make(tea.BatchMsg, 0, len(batch))
			for _, child := range batch {
				if next := findInputCommand(session, request, child); next != nil {
					wrapped = append(wrapped, next)
				}
			}
			return wrapped
		}
		return findInputResult{session, request, msg}
	}
}

func (m *model) openFind() tea.Cmd {
	p := m.pager
	if p.doc == nil {
		p.notice = "Wait for source text before finding a passage."
		return nil
	}
	p.playing, p.waiting = false, false
	p.gen = m.nextSeq()
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "Exact source text"
	input.SetVirtualCursor(false)
	input.SetStyles(textinput.Styles{Cursor: textinput.CursorStyle{Blink: false}})
	input.SetWidth(max(1, m.pagerWidth()-4))
	representation := "epub-text/2"
	if p.representation == "pdf-text/1" {
		representation = "pdf-text/1"
	}
	p.finder = &readerFind{input: input, representation: representation, editing: true, request: m.nextSeq(), notice: "Exact and case sensitive, including extracted whitespace. One line, up to 1000 UTF-8 bytes."}
	return tea.Batch(findInputCommand(p.session, p.finder.request, p.finder.input.Focus()), tea.ClearScreen)
}

// Queries stay literal. Reject unsupported input instead of silently applying
// the editor's control/newline sanitization or truncating a pasted quotation.
func validFindInput(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return false
		}
	}
	return true
}

func (m *model) findInput(message tea.Msg) tea.Cmd {
	f := m.pager.finder
	insert := ""
	switch msg := message.(type) {
	case tea.KeyPressMsg:
		switch msg.Code {
		case tea.KeyEscape:
			m.closeFind()
			return tea.ClearScreen
		case tea.KeyEnter:
			return m.submitFind()
		}
		insert = msg.Text
	case tea.PasteMsg:
		insert = msg.Content
	}
	if len(f.input.Value())+len(insert) > maxFindBytes {
		f.notice = "Input not inserted: limit 1000 UTF-8 bytes."
		return nil
	}
	if !validFindInput(insert) {
		f.notice = "Input not inserted: use one line without control or replacement characters."
		return nil
	}
	before, pos := f.input.Value(), f.input.Position()
	var cmd tea.Cmd
	if insert != "" {
		// Text commits and pastes are literal, even when their text is a key
		// name. Leave editing/navigation shortcuts to the existing editor.
		runes := []rune(before)
		pos = min(len(runes), max(0, pos))
		f.input.SetValue(string(runes[:pos]) + insert + string(runes[pos:]))
		f.input.SetCursor(pos + utf8.RuneCountInString(insert))
	} else {
		f.input, cmd = f.input.Update(message)
	}
	if len(f.input.Value()) > maxFindBytes || !validFindInput(f.input.Value()) {
		f.input.SetValue(before)
		f.input.SetCursor(pos)
		f.notice = "Search accepts one line of at most 1000 UTF-8 bytes. The input was not inserted."
		return nil
	}
	f.notice = "Exact and case sensitive, including extracted whitespace. Enter searches; Escape keeps the passage."
	return findInputCommand(m.pager.session, f.request, cmd)
}

func (m *model) submitFind() tea.Cmd {
	p, f := m.pager, m.pager.finder
	query := f.input.Value()
	if query == "" || len(query) > maxFindBytes || !validFindInput(query) {
		f.notice = "Enter one exact line of 1-1000 UTF-8 bytes."
		return nil
	}
	if f.cancel != nil {
		f.cancel()
	}
	f.query, f.editing, f.fetching, f.matches, f.selected = query, false, true, nil, 0
	f.unsupported = 0
	f.input.Blur()
	f.request = m.nextSeq()
	f.notice = "Searching the book's extracted text. Escape keeps the passage unchanged."
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	f.cancel = cancel
	session, request, service := p.session, f.request, m.service
	req := content.Request{SchemaVersion: 1, Catalog: p.catalog, Root: p.root, AssetID: p.asset, Representation: f.representation, Find: query, MaxBytes: 4096}
	return func() tea.Msg {
		defer cancel()
		data, err := service.Content(ctx, req)
		return findResult{session, request, data, err}
	}
}

func (m *model) findResult(msg findResult) {
	p := m.pager
	if p == nil || p.session != msg.session || p.finder == nil || p.finder.request != msg.request || !p.finder.fetching {
		return
	}
	f := p.finder
	f.fetching = false
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
	if msg.err != nil {
		f.notice = "Search unavailable: " + present.Error(msg.err)
		return
	}
	switch msg.data.Status {
	case "no_matches", "matches_found", "matches_truncated":
	default:
		f.notice = "Search did not return supported extracted-text matches."
		return
	}
	f.matches = msg.data.Matches
	f.unsupported = len(msg.data.UnsupportedUnits)
	truncated := msg.data.Status == "matches_truncated" || len(f.matches) > maxFindMatches
	if len(f.matches) > maxFindMatches {
		f.matches = f.matches[:maxFindMatches]
	}
	label := "matches"
	if len(f.matches) == 1 {
		label = "match"
	}
	f.notice = fmt.Sprintf("%d exact %s in extracted text. Omitted content is not searched.", len(f.matches), label)
	if truncated {
		f.notice = "First 100 matches only; narrow the query. Omitted content is not searched."
	}
	if len(msg.data.UnsupportedUnits) > 0 {
		f.notice += fmt.Sprintf(" %d unsupported unit(s).", len(msg.data.UnsupportedUnits))
	}
}

func (m *model) closeFind() {
	if f := m.pager.finder; f != nil && f.cancel != nil {
		f.cancel()
	}
	m.pager.finder = nil
}

func (m *model) findKey(key string) tea.Cmd {
	p, f := m.pager, m.pager.finder
	switch key {
	case "/":
		if f.cancel != nil {
			f.cancel()
			f.cancel = nil
		}
		f.request = m.nextSeq()
		f.fetching, f.editing = false, true
		f.matches = nil
		f.notice = "Edit the exact query. Enter searches; Escape keeps the passage unchanged."
		return tea.Batch(findInputCommand(p.session, f.request, f.input.Focus()), tea.ClearScreen)
	case "j", "down":
		f.selected = min(max(0, len(f.matches)-1), f.selected+1)
	case "k", "up":
		f.selected = max(0, f.selected-1)
	case "space", "pgdown":
		f.selected = min(max(0, len(f.matches)-1), f.selected+max(1, m.pagerBody()-4))
	case "b", "pgup":
		f.selected = max(0, f.selected-max(1, m.pagerBody()-4))
	case "g", "home":
		f.selected = 0
	case "G", "end":
		f.selected = max(0, len(f.matches)-1)
	case "enter":
		if f.fetching || len(f.matches) == 0 {
			return nil
		}
		match := f.matches[f.selected]
		if match.Cursor == "" {
			f.notice = "This match has no source-bound reference; nothing was opened."
			return nil
		}
		catalog, root, asset, representation := p.catalog, p.root, p.asset, f.representation
		speed, rate, only := p.speed, p.rate, p.only
		cmd := m.openPager(catalog, root, asset, representation, reader.Position{Unit: match.Unit, Offset: match.Start}, speed, rate)
		m.pager.initialCursor, m.pager.only = match.Cursor, only
		m.pager.notice = "Opened an exact whole-book match by your choice; paused."
		return tea.Batch(cmd, tea.ClearScreen)
	}
	return nil
}

// shown is the drawn copy of the query: visible escapes must not become a
// different search query, and the real cursor follows this copy.
func (f *readerFind) shown() textinput.Model {
	shown := f.input
	raw := []rune(f.input.Value())
	pos := min(len(raw), max(0, f.input.Position()))
	shown.CharLimit = 0
	shown.SetValue(textsafe.Visible(string(raw)))
	shown.SetCursor(len([]rune(textsafe.Visible(string(raw[:pos])))))
	return shown
}

// findCursor is the real cursor in the find field: below the reader header
// and the find title, after the centered margin and "> " (decision 0094).
func (m *model) findCursor() *tea.Cursor {
	p := m.pager
	if p == nil || p.finder == nil || !p.finder.editing || p.help {
		return nil
	}
	return inputCursor(p.finder.shown(), max(0, (m.width-m.pagerWidth())/2)+2, 2, m.width)
}

func (m *model) findBody(margin string) []string {
	f := m.pager.finder
	lines := []string{margin + "Find exact text in the whole book"}
	if f.editing {
		lines = append(lines, margin+"> "+f.shown().View())
		return lines
	}
	lines = append(lines, margin+"Query: "+strconv.Quote(f.query))
	coverage := "Extracted text only; omissions excluded."
	if f.unsupported > 0 {
		coverage = fmt.Sprintf("%d units unsupported; extracted text only.", f.unsupported)
	}
	lines = append(lines, margin+coverage)
	body := max(1, m.pagerBody()-4)
	top := f.selected / body * body
	for i := top; i < min(len(f.matches), top+body); i++ {
		match := f.matches[i]
		label := fmt.Sprintf("Unit %d, bytes %d..%d", match.Unit, match.Start, match.End)
		if f.representation == "pdf-text/1" {
			label = fmt.Sprintf("Physical page %d; %s", match.Unit+1, label)
			if match.PageLabel != "" {
				label += "; declared label " + present.Line(match.PageLabel)
			}
		}
		mark := "  "
		if i == f.selected {
			mark = "> "
		}
		lines = append(lines, margin+mark+label)
	}
	return lines
}

func findHelp() []string {
	return []string{"Find keys", "j/k, Down/Up  choose a match", "Space, PageDown  next page of matches", "b, PageUp        previous page of matches", "g/Home, G/End    first or last match", "Enter           revalidate and open the exact source reference", "/               edit the query", "q, Escape       close the reader while help is shown", "?               return to matches"}
}
