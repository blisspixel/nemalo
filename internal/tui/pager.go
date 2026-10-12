package tui

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/reader"
)

const (
	// defaultMeasure is the EPUB text width in columns. Comprehension was best
	// near 55 characters per line in screen studies, and accessibility
	// guidance asks for a way to keep lines within 80. PDF text keeps the
	// source's typeset lines, often near 90 characters, so it starts wider to
	// avoid splitting each one; - narrows either.
	defaultMeasure = 66
	pdfMeasure     = 100
	minMeasure     = 24
	maxMeasure     = 120
	// speedAhead and pageAhead keep delivered text ahead of the reader.
	speedAhead = 400
	pagesAhead = 3
)

// pager shows delivered source text a page or a word at a time. Its position
// lives only in this session; closing it reports the position and records
// nothing.
type pager struct {
	catalog, root, asset, representation string
	start                                reader.Position
	units                                []content.Unit
	doc                                  *reader.Doc
	layout                               reader.Layout
	top                                  int
	word                                 int
	measure                              int
	rate                                 int
	speed, playing, waiting              bool
	// blocked stops continuing after a range failed; failed is its unit and
	// failure the explanation, shown once the reader reaches the end of the
	// delivered text rather than when read-ahead meets it.
	blocked  bool
	failed   int
	failedAt reader.Position
	failure  string
	// resume is where a new session continues after this one reached its
	// text limit.
	resume   *reader.Position
	fetching bool
	request  int
	gen      int
	cancel   context.CancelFunc
	notice   string
	// only closes the program with the reader (nemalo read).
	only bool
}

type pagerChunk struct {
	request int
	units   bool
	data    content.Result
	err     error
}

type pagerTick struct{ gen int }

var fullNotice = fmt.Sprintf("This session holds its %d MiB text limit. ] continues from here in a new session.", reader.MaxText>>20)

// nextSeq numbers fetches and scheduled words across every reader session,
// so a message from a closed or replaced session never matches a new one.
func (m *model) nextSeq() int {
	m.pagerSeq++
	return m.pagerSeq
}

// after schedules the next word; tests replace it to step time by hand.
var after = tea.Tick

// openPager starts a session at start. representation is epub-text/2 or
// pdf-text/1; the units listing comes first so headings can name units.
func (m *model) openPager(catalog, root, asset, representation string, start reader.Position, speed bool, rate int) tea.Cmd {
	measure := defaultMeasure
	if representation == "pdf-text/1" {
		measure = pdfMeasure
	}
	m.pager = &pager{catalog: catalog, root: root, asset: asset, representation: representation, start: start, measure: measure, rate: min(max(rate, reader.MinRate), reader.MaxRate), speed: speed, failed: -1}
	m.pager.notice = "Retrieving source text. Nothing about this reading is recorded."
	return m.pagerFetch(content.Request{UnitsOnly: true}, true)
}

// ReadOptions opens the reader directly on one asset.
type ReadOptions struct {
	Catalog, Root, Asset string
	Start                reader.Position
	Speed                bool
	Rate                 int
}

// Read runs only the reader and returns the position it closed at, so the
// caller can show how to return there. Nothing is recorded.
func Read(ctx context.Context, service app.Service, in io.Reader, out io.Writer, o ReadOptions) (*reader.Position, error) {
	units, err := service.TextUnits(ctx, o.Catalog, o.Root, o.Asset)
	if err != nil {
		return nil, err
	}
	m := newModel(ctx, service)
	m.mode = "read"
	m.initial = m.openPager(o.Catalog, o.Root, o.Asset, units.Representation, o.Start, o.Speed, o.Rate)
	m.pager.only = true
	defer func() {
		if m.pager != nil && m.pager.cancel != nil {
			m.pager.cancel()
		}
	}()
	if _, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out)).Run(); err != nil {
		return nil, err
	}
	return m.readerStop, nil
}

func (m *model) pagerFetch(req content.Request, units bool) tea.Cmd {
	p := m.pager
	if p.cancel != nil {
		p.cancel()
	}
	req.SchemaVersion, req.Catalog, req.Root, req.AssetID, req.Representation, req.MaxBytes = 1, p.catalog, p.root, p.asset, p.representation, 65536
	ctx, cancel := context.WithCancel(m.ctx)
	p.cancel = cancel
	p.request = m.nextSeq()
	p.fetching = true
	id, service := p.request, m.service
	return func() tea.Msg {
		data, err := service.Content(ctx, req)
		return pagerChunk{request: id, units: units, data: data, err: err}
	}
}

// pagerJump starts a new session at the start of unit, an explicit choice
// that the heading and notice disclose.
func (m *model) pagerJump(unit int) tea.Cmd {
	p := m.pager
	if p.units == nil || unit < 0 || unit >= len(p.units) {
		p.notice = "No unit there."
		return nil
	}
	note := ""
	if p.blocked && p.failed >= 0 && unit == p.failed+1 {
		if p.failedAt.Offset > 0 {
			note = "Opened by your choice; the rest of " + p.doc.Label(p.failed) + " from byte " + strconv.Itoa(p.failedAt.Offset) + " was not read. "
		} else {
			note = "Opened by your choice after " + p.doc.Label(p.failed) + ", which was not read. "
		}
	}
	return m.pagerOpen(reader.Position{Unit: unit}, note)
}

// pagerOpen starts a new session at at, discarding the current one.
func (m *model) pagerOpen(at reader.Position, note string) tea.Cmd {
	p := m.pager
	p.doc, p.layout, p.start, p.resume = reader.New(p.representation, p.units), reader.Layout{}, at, nil
	p.doc.StartAt(at)
	p.top, p.word, p.blocked, p.failed, p.failure, p.waiting, p.playing = 0, 0, false, -1, "", false, false
	p.gen = m.nextSeq()
	p.notice = note + "Retrieving " + p.doc.Label(at.Unit) + "."
	return m.pagerFetch(content.Request{Unit: at.Unit, Offset: at.Offset}, false)
}

func (m *model) pagerChunk(msg pagerChunk) tea.Cmd {
	p := m.pager
	if p == nil || msg.request != p.request {
		return nil
	}
	p.fetching = false
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	if msg.err != nil {
		return m.pagerFailed(msg)
	}
	if msg.units {
		p.units = msg.data.Units
		if p.start.Unit >= len(p.units) {
			p.blocked, p.notice = true, fmt.Sprintf("The source has %d unit(s); unit %d does not exist.", len(p.units), p.start.Unit)
			return nil
		}
		p.doc = reader.New(p.representation, p.units)
		p.doc.StartAt(p.start)
		return m.pagerFetch(content.Request{Unit: p.start.Unit, Offset: p.start.Offset}, false)
	}
	before := len(p.doc.Items)
	if err := p.doc.Append(msg.data); err != nil {
		p.doc.Finish()
		m.pagerLayout()
		p.blocked, p.playing, p.waiting, p.failed = true, false, false, p.doc.Expect().Unit
		p.failure = fmt.Sprintf("Stopped: %q. Nothing was skipped; ] opens the next unit.", err.Error())
		return nil
	}
	if strings.HasPrefix(p.notice, "Retrieving") {
		p.notice = ""
	}
	m.pagerLayout()
	// Playback that ran out of text resumes at the first new word, or keeps
	// waiting when the range only began a word.
	if p.speed && p.playing && p.word >= before {
		p.word = p.doc.NextWord(before)
		if p.word < len(p.doc.Items) {
			p.waiting = false
			return tea.Batch(m.pagerSchedule(), m.pagerMore())
		}
		return m.pagerAwait()
	}
	return m.pagerMore()
}

func (m *model) pagerFailed(msg pagerChunk) tea.Cmd {
	p := m.pager
	p.blocked, p.playing, p.waiting = true, false, false
	at := p.start
	if p.doc != nil && len(p.doc.Items) > 0 {
		at = p.doc.Expect()
	}
	p.failed, p.failedAt = at.Unit, at
	label := "Unit " + strconv.Itoa(at.Unit+1)
	if p.doc != nil {
		// Show everything delivered before the failure, then stop.
		p.doc.Finish()
		m.pagerLayout()
		label = p.doc.Label(at.Unit)
	}
	if strings.HasPrefix(p.notice, "Retrieving") {
		p.notice = ""
	}
	if msg.units {
		p.failure = fmt.Sprintf("No text representation could list this source: %q. Close with q.", msg.err.Error())
		return nil
	}
	reason := fmt.Sprintf("could not be read: %q", msg.err.Error())
	if at.Offset > 0 {
		reason = fmt.Sprintf("could not be read from byte %d: %q", at.Offset, msg.err.Error())
	}
	if msg.data.Status == "unsupported_extraction" {
		reason = "has no supported text"
	}
	p.failure = label + " " + reason + ". Nothing was skipped; ] opens the next unit."
	return nil
}

// pagerMore continues delivery when the reader nears the end of the text.
func (m *model) pagerMore() tea.Cmd {
	p := m.pager
	if p.fetching || p.blocked || p.doc == nil || p.doc.End || p.doc.Next == "" {
		return nil
	}
	if p.doc.Full() {
		m.pagerFull()
		return nil
	}
	if p.speed && len(p.doc.Items)-p.word > speedAhead || !p.speed && len(p.layout.Lines)-p.top > pagesAhead*m.pagerBody() {
		return nil
	}
	return m.pagerFetch(content.Request{Cursor: p.doc.Next}, false)
}

// pagerFull stops a session at its text limit, keeping where a new one
// continues without losing or repeating a word.
func (m *model) pagerFull() {
	p := m.pager
	if p.resume == nil {
		at := p.doc.Stop()
		p.resume = &at
		m.pagerLayout()
	}
	p.notice = fullNotice
}

func (m *model) pagerBody() int { return max(1, m.height-4) }

func (m *model) pagerWidth() int {
	return max(minMeasure, min(m.pager.measure, m.width-4))
}

// pagerLayout extends the layout, keeping the top row's place across a
// width change.
func (m *model) pagerLayout() {
	p := m.pager
	if p.doc == nil {
		return
	}
	width := m.pagerWidth()
	if p.layout.Width != width && len(p.layout.Lines) > 0 {
		at := p.layout.Lines[min(p.top, len(p.layout.Lines)-1)].Pos
		p.layout.Update(p.doc, width)
		p.top = p.layout.LineFor(at)
		return
	}
	p.layout.Update(p.doc, width)
}

func (m *model) pagerSchedule() tea.Cmd {
	p := m.pager
	p.gen = m.nextSeq()
	if p.word >= len(p.doc.Items) {
		return nil
	}
	gen := p.gen
	return after(reader.Dwell(p.doc.Items[p.word], p.rate), func(time.Time) tea.Msg { return pagerTick{gen: gen} })
}

func (m *model) pagerTick(msg pagerTick) tea.Cmd {
	p := m.pager
	if p == nil || msg.gen != p.gen || !p.playing {
		return nil
	}
	p.word = p.doc.NextWord(p.word + 1)
	if p.word < len(p.doc.Items) {
		return tea.Batch(m.pagerSchedule(), m.pagerMore())
	}
	return m.pagerAwait()
}

// pagerAwait handles playback reaching the end of delivered text.
func (m *model) pagerAwait() tea.Cmd {
	p := m.pager
	p.word = len(p.doc.Items)
	switch {
	case p.doc.End:
		p.playing, p.notice = false, "End of the source."
	case p.blocked || p.doc.Full() || p.doc.Next == "":
		p.playing, p.waiting = false, false
		if p.doc.Full() {
			m.pagerFull()
		}
	default:
		p.waiting = true
		return m.pagerMore()
	}
	return nil
}

// pagerAtEnd reports that the reader has reached the end of the delivered text.
func (m *model) pagerAtEnd() bool {
	p := m.pager
	if p.doc == nil {
		return true
	}
	if p.speed {
		return p.word >= len(p.doc.Items)
	}
	return p.top+m.pagerBody() >= len(p.layout.Lines)
}

// pagerPosition is the source position on screen: the first word of the
// page, or the word shown.
func (m *model) pagerPosition() (reader.Position, bool) {
	p := m.pager
	if p == nil || p.doc == nil || len(p.doc.Items) == 0 {
		return reader.Position{}, false
	}
	if p.speed {
		i := min(p.word, len(p.doc.Items)-1)
		return p.doc.Items[i].Pos, true
	}
	if len(p.layout.Lines) == 0 {
		return reader.Position{}, false
	}
	return p.layout.Lines[min(p.top, len(p.layout.Lines)-1)].Pos, true
}

func (m *model) closePager() tea.Cmd {
	p := m.pager
	if p.cancel != nil {
		p.cancel()
	}
	m.status = "Closed the reader. Nothing about this reading was recorded."
	if at, ok := m.pagerPosition(); ok {
		m.readerStop = &at
		m.status = fmt.Sprintf("Closed the reader at unit %d, byte %d (%s). Nothing about this reading was recorded.", at.Unit, at.Offset, p.doc.Label(at.Unit))
	}
	only := p.only
	m.pager = nil
	m.resize()
	if only {
		return tea.Quit
	}
	return nil
}

func (m *model) pagerKey(key string) tea.Cmd {
	p := m.pager
	switch key {
	case "q", "esc":
		return m.closePager()
	case "]", "[":
		unit := p.start.Unit
		if at, ok := m.pagerPosition(); ok {
			unit = at.Unit
		}
		if key == "]" {
			// At the end of a session that reached its limit, continue there.
			if p.resume != nil && m.pagerAtEnd() {
				return m.pagerOpen(*p.resume, "Continued in a new session. ")
			}
			unit++
			// At the end of the text, the next unit after an unreadable one.
			if p.blocked && unit == p.failed && m.pagerAtEnd() {
				unit++
			}
		} else {
			unit--
		}
		return m.pagerJump(unit)
	case "+", "=", "-":
		if !p.speed {
			step := 4
			if key == "-" {
				step = -4
			}
			p.measure = max(minMeasure, min(maxMeasure, min(p.measure, m.width-4)+step))
			m.pagerLayout()
			return m.pagerMore()
		}
	}
	if p.doc == nil {
		return nil
	}
	if p.speed {
		return m.speedKey(key)
	}
	return m.pageKey(key)
}

func (m *model) pageKey(key string) tea.Cmd {
	p := m.pager
	body, lines := m.pagerBody(), len(p.layout.Lines)
	switch key {
	case "space", "pgdown", "right", "l", "n":
		if p.top+body < lines {
			p.top += body
		} else if p.doc.End {
			p.notice = "End of the source."
		} else if !p.blocked {
			p.notice = "Retrieving the next range."
		}
	case "b", "pgup", "left", "h":
		if p.top == 0 {
			p.notice = "Start of this session. [ opens the previous unit."
		}
		p.top = max(0, p.top-body)
	case "down", "j":
		if p.top+1 < lines {
			p.top++
		}
	case "up", "k":
		p.top = max(0, p.top-1)
	case "home", "g":
		p.top = 0
	case "end", "G":
		p.top = max(0, lines-body)
	case "s":
		at, _ := m.pagerPosition()
		p.speed, p.playing, p.word = true, false, p.doc.ItemFor(at)
		p.gen = m.nextSeq()
		p.notice = "Paused. Space starts at " + strconv.Itoa(p.rate) + " words per minute."
	}
	return m.pagerMore()
}

func (m *model) speedKey(key string) tea.Cmd {
	p := m.pager
	switch key {
	case "space":
		if p.playing {
			p.playing, p.waiting = false, false
			p.gen = m.nextSeq()
			return nil
		}
		p.playing, p.notice = true, ""
		if p.word >= len(p.doc.Items) {
			return m.pagerAwait()
		}
		return tea.Batch(m.pagerSchedule(), m.pagerMore())
	case "left", "h":
		p.word = p.doc.PreviousSentence(p.word)
	case "right", "l":
		next := p.doc.NextSentence(p.word)
		if next >= len(p.doc.Items) {
			return m.pagerAwait()
		}
		p.word = next
	case "up", "k", "+", "=":
		p.rate = min(reader.MaxRate, p.rate+reader.RateStep)
	case "down", "j", "-":
		p.rate = max(reader.MinRate, p.rate-reader.RateStep)
	case "p":
		at, _ := m.pagerPosition()
		p.speed, p.playing, p.waiting = false, false, false
		p.gen = m.nextSeq()
		p.top = p.layout.LineFor(at)
		return m.pagerMore()
	default:
		return nil
	}
	if !p.playing {
		return m.pagerMore()
	}
	if p.word >= len(p.doc.Items) {
		// Still waiting for text; the arriving range resumes playback.
		return m.pagerAwait()
	}
	p.waiting = false
	return tea.Batch(m.pagerSchedule(), m.pagerMore())
}

// pagerView fills the terminal: a header naming the unit, the text, and a
// footer with the position, any notice, and keys.
func (m *model) pagerView() string {
	p := m.pager
	width := m.pagerWidth()
	margin := strings.Repeat(" ", max(0, (m.width-width)/2))
	at, ok := m.pagerPosition()
	header := "Nemalo reader"
	position := ""
	if ok {
		header += "  " + p.doc.Label(at.Unit)
		position = fmt.Sprintf("Unit %d, byte %d", at.Unit, at.Offset)
		if at.Unit < len(p.units) && p.units[at.Unit].TextBytes > 0 {
			position += fmt.Sprintf(" of %d", p.units[at.Unit].TextBytes)
		}
	}
	var body []string
	if p.speed {
		body = m.speedBody(width, margin)
		state := "paused"
		if p.playing {
			state = "playing"
		}
		position = fmt.Sprintf("%d wpm nominal, %s  %s", p.rate, state, position)
	} else if p.doc != nil {
		end := min(len(p.layout.Lines), p.top+m.pagerBody())
		for _, line := range p.layout.Lines[min(p.top, end):end] {
			body = append(body, margin+m.pagerLine(line, reader.Position{}, false))
		}
	}
	for len(body) < m.pagerBody() {
		body = append(body, "")
	}
	notice := p.notice
	if p.failure != "" && m.pagerAtEnd() {
		notice = p.failure
	}
	if p.speed && p.rate > reader.SkimRate && notice == "" {
		notice = "Above 350 words per minute, studies found comprehension falls; treat this as skimming."
	}
	keys := "Space next  b back  j/k line  [ ] unit  +/- width  s speed  q close"
	if p.speed {
		keys = "Space play/pause  Left/Right sentence  Up/Down rate  [ ] unit  p pages  q close"
	}
	lines := []string{m.paint(clip(header, m.width-2), muted, false)}
	lines = append(lines, body[:m.pagerBody()]...)
	lines = append(lines, m.paint(clip(position, m.width-2), muted, false), m.paint(clip(notice, m.width-2), warning, false), m.paint(clip(keys, m.width-2), muted, false))
	return strings.Join(lines, "\n")
}

// pagerLine renders a row; in speed mode the word at mark is emphasized.
func (m *model) pagerLine(line reader.Line, mark reader.Position, marked bool) string {
	switch line.Kind {
	case reader.Heading:
		return m.paint(m.pager.layout.Text(line), accent, true)
	case reader.Gap:
		return m.paint(m.pager.layout.Text(line), warning, false)
	}
	indent := ""
	if line.Hang {
		indent = strings.Repeat(" ", reader.Indent)
	}
	if !marked {
		return indent + m.pager.layout.Text(line)
	}
	var b strings.Builder
	b.WriteString(indent)
	for i, it := range m.pager.layout.Words(line) {
		if i > 0 && !it.Join {
			b.WriteByte(' ')
		}
		if it.Pos == mark {
			b.WriteString(m.emphasize(it.Text))
			continue
		}
		b.WriteString(it.Text)
	}
	return b.String()
}

// emphasize marks text without color changes that could flash: bold and
// underline, or brackets when styling is off.
func (m *model) emphasize(text string) string {
	if !m.color {
		return "[" + text + "]"
	}
	return lipgloss.NewStyle().Bold(true).Underline(true).Render(text)
}

// speedBody shows one word with its focus letter in a fixed column, between
// fixed guide marks, so nothing on screen moves but the word. Paused, the
// rows around the word follow.
func (m *model) speedBody(width int, margin string) []string {
	p := m.pager
	body := m.pagerBody()
	rows := make([]string, 0, body)
	column := width / 3
	guide := margin + strings.Repeat(" ", column) + m.paint("|", muted, false)
	for range max(0, body/2-3) {
		rows = append(rows, "")
	}
	word := ""
	if p.doc != nil && p.word < len(p.doc.Items) {
		it := p.doc.Items[p.word]
		switch it.Kind {
		case reader.Word:
			before, focus, after := reader.Focus(it.Text)
			if !m.color {
				// Without styling, the guide marks alone show the focus column.
				word = margin + strings.Repeat(" ", max(0, column-reader.Width(before))) + before + focus + after
			} else {
				word = margin + strings.Repeat(" ", max(0, column-reader.Width(before))) + before + m.paint(focus, accent, true) + after
			}
		default:
			color := warning
			if it.Kind == reader.Heading {
				color = accent
			}
			word = margin + m.paint(clip(it.Text, width), color, it.Kind == reader.Heading)
		}
	} else if p.waiting {
		word = margin + m.paint("Retrieving the next range.", muted, false)
	}
	rows = append(rows, guide, word, guide, "")
	if !p.playing && p.doc != nil && len(p.layout.Lines) > 0 && p.word < len(p.doc.Items) {
		at := p.doc.Items[p.word].Pos
		i := p.layout.RowOf(at)
		for _, line := range p.layout.Lines[max(0, i-1):min(len(p.layout.Lines), i+3)] {
			rows = append(rows, margin+m.pagerLine(line, at, true))
		}
	}
	return rows[:min(len(rows), body)]
}
