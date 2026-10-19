package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/present"
	"github.com/blisspixel/nemalo/internal/reader"
)

// Notes are a detour from the reader into a source note and back. The main
// passage is never replaced: its session, page, and word stay as they were,
// and Escape returns to them. Links come from epub-structure/1, whose text
// coordinates the reader's epub-text/2 shares; only source-declared targets
// within the book are followed, and nothing is recorded.

// maxNoteDepth bounds nested detours; the main passage is always one Escape
// per level away.
const maxNoteDepth = 4

// noteLink is one link in the shown passage. A note reference is declared
// by the source (epub:type noteref); any other link is listed as a link and
// is not presumed to be a note.
type noteLink struct {
	label, href, status string
	noteref, cyclic     bool
	reference           string
	targetUnit          int
	targetPart          string
}

// noteFrame is one note being read: a bounded part, continued explicitly.
type noteFrame struct {
	link    noteLink
	doc     *reader.Doc
	layout  reader.Layout
	top     int
	results []content.Result
	next    string
	// note discloses how the shown part relates to the link's target.
	note    string
	failure string
}

type notesState struct {
	// listing shows links to choose from; links are for the top frame, or
	// for the main passage when there is no frame.
	listing  bool
	links    []noteLink
	selected int
	// segments are the passage ranges still to list links for.
	segments []reader.Position
	ends     []int
	frames   []*noteFrame
	request  int
	cancel   context.CancelFunc
	fetching bool
	notice   string
}

type noteChunk struct {
	request int
	kind    string
	data    content.Result
	err     error
}

func (n *notesState) active() bool { return n != nil && (n.listing || len(n.frames) > 0) }

func (n *notesState) top() *noteFrame {
	if len(n.frames) == 0 {
		return nil
	}
	return n.frames[len(n.frames)-1]
}

// openLinks lists the links in the shown page of the main passage or of the
// note on screen. Word mode pauses first.
func (m *model) openLinks() tea.Cmd {
	p := m.pager
	if p.representation == "pdf-text/1" {
		p.notice = "PDF text has no source links to follow."
		return nil
	}
	if p.notes == nil {
		p.notes = &notesState{}
	}
	n := p.notes
	if p.speed && p.playing {
		p.playing, p.waiting = false, false
		p.gen = m.nextSeq()
	}
	if f := n.top(); f != nil {
		n.links, n.selected, n.listing = frameLinks(f), 0, true
		n.notice = ""
		if len(n.links) == 0 {
			n.notice = "This note has no links."
		}
		return nil
	}
	segments, ends := m.passageSegments()
	if len(segments) == 0 {
		p.notice = "No text is shown yet."
		return nil
	}
	n.links, n.selected, n.listing, n.segments, n.ends, n.notice = nil, 0, true, segments, ends, "Finding links in this passage."
	return m.nextLinkSegment()
}

// passageSegments are the unit ranges the page on screen shows, or the
// sentence around the word in word mode.
func (m *model) passageSegments() ([]reader.Position, []int) {
	p := m.pager
	if p.doc == nil || len(p.doc.Items) == 0 || !p.speed && len(p.layout.Lines) == 0 {
		return nil, nil
	}
	var from, to reader.Position
	if p.speed {
		i := min(p.word, len(p.doc.Items)-1)
		start, end := p.doc.SentenceStart(i), p.doc.NextSentence(i)
		from = p.doc.Items[start].Pos
		to = p.doc.Expect()
		if end < len(p.doc.Items) && from.Less(p.doc.Items[end].Pos) {
			to = p.doc.Items[end].Pos
		}
	} else {
		top := min(p.top, len(p.layout.Lines)-1)
		from = p.layout.Lines[top].Pos
		to = p.doc.Expect()
		if next := top + m.pagerBody(); next < len(p.layout.Lines) {
			to = p.layout.Lines[next].Pos
		}
	}
	var starts []reader.Position
	var ends []int
	for unit := from.Unit; unit <= to.Unit; unit++ {
		start, end := 0, -1
		if unit == from.Unit {
			start = from.Offset
		}
		if unit == to.Unit {
			end = to.Offset
			if end <= start {
				continue
			}
		}
		starts, ends = append(starts, reader.Position{Unit: unit, Offset: start}), append(ends, end)
	}
	return starts, ends
}

// nextLinkSegment requests the structured parts of the next passage range.
func (m *model) nextLinkSegment() tea.Cmd {
	n := m.pager.notes
	if len(n.segments) == 0 {
		n.fetching, n.notice = false, ""
		if len(n.links) == 0 {
			n.notice = "No links in this passage."
		}
		return nil
	}
	at, end := n.segments[0], n.ends[0]
	n.segments, n.ends = n.segments[1:], n.ends[1:]
	size := 65536
	if end >= 0 {
		size = min(size, max(4, end-at.Offset))
	}
	return m.noteFetch("links", content.Request{Unit: at.Unit, Offset: at.Offset, MaxBytes: size})
}

func (m *model) noteFetch(kind string, req content.Request) tea.Cmd {
	p := m.pager
	n := p.notes
	if n.cancel != nil {
		n.cancel()
	}
	req.SchemaVersion, req.Catalog, req.Root, req.AssetID, req.Representation = 1, p.catalog, p.root, p.asset, "epub-structure/1"
	if req.MaxBytes == 0 {
		req.MaxBytes = 16384
	}
	ctx, cancel := context.WithCancel(m.ctx)
	n.cancel, n.request, n.fetching = cancel, m.nextSeq(), true
	id, service := n.request, m.service
	return func() tea.Msg {
		data, err := service.Content(ctx, req)
		return noteChunk{request: id, kind: kind, data: data, err: err}
	}
}

// linksIn lists the links a structured range delivered, in source order.
func linksIn(r content.Result) []noteLink {
	var out []noteLink
	if r.Locator == nil {
		return nil
	}
	for _, part := range r.Parts {
		if part.Link == nil || part.Start < r.Locator.Start || part.Start >= r.Locator.End {
			continue
		}
		label := ""
		if part.End <= r.Locator.End {
			label = r.Text[part.Start-r.Locator.Start : part.End-r.Locator.Start]
		}
		label = present.Line(label)
		if label == "" {
			label = "(no link text)"
		}
		out = append(out, noteLink{label: label, href: part.Link.Href, status: part.Link.Status, noteref: part.Kind == "noteref", cyclic: part.Link.Cyclic, reference: part.Link.Reference, targetUnit: part.Link.TargetUnit, targetPart: part.Link.TargetPart})
	}
	return out
}

func frameLinks(f *noteFrame) []noteLink {
	var out []noteLink
	for _, r := range f.results {
		out = append(out, linksIn(r)...)
	}
	return out
}

func (m *model) noteChunk(msg noteChunk) tea.Cmd {
	p := m.pager
	if p == nil || p.notes == nil || msg.request != p.notes.request {
		return nil
	}
	n := p.notes
	n.fetching = false
	if n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
	switch msg.kind {
	case "links":
		if msg.err != nil {
			n.segments, n.ends = nil, nil
			n.notice = "Links are not available here: " + present.Error(msg.err) + ". The passage is unchanged."
			return nil
		}
		n.links = append(n.links, linksIn(msg.data)...)
		return m.nextLinkSegment()
	case "note", "enclosing", "more":
		f := n.top()
		if f == nil {
			return nil
		}
		if msg.err != nil {
			f.failure = "This note could not be read: " + present.Error(msg.err) + ". Escape returns to the passage."
			if f.doc != nil {
				f.doc.Finish()
			}
			return nil
		}
		if msg.kind == "note" {
			if parent, ok := enclosingPart(msg.data, f.link.targetPart); ok {
				f.note = "The link points to a marker, " + present.Line(f.link.targetPart) + "; showing the part it is in, " + present.Line(parent) + "."
				return m.noteFetch("enclosing", content.Request{Unit: f.link.targetUnit, Part: parent})
			}
		}
		if msg.kind != "more" {
			f.doc = reader.New("epub-structure/1", p.units)
			if msg.data.Locator != nil {
				f.doc.StartAt(reader.Position{Unit: msg.data.Locator.Unit, Offset: msg.data.Locator.Start})
			}
		}
		if err := f.doc.Append(msg.data); err != nil {
			f.doc.Finish()
			f.failure = "This note stopped: " + present.Error(err) + ". Escape returns to the passage."
			return nil
		}
		f.results = append(f.results, msg.data)
		f.next = msg.data.Continuation
		// A whole-document target is read within its own document only.
		if f.link.targetPart == "" && msg.data.EndOfUnit {
			f.next = ""
		}
		if f.next == "" {
			f.doc.Finish()
		}
		m.noteLayout()
	}
	return nil
}

// enclosingPart reports the part a marker target sits in. A link often points
// to an empty or one-label anchor at the start of a note's paragraph; reading
// only the anchor would show "[1]". The enclosing part is the source's own
// structure, and the frame says it is shown instead.
func enclosingPart(r content.Result, target string) (string, bool) {
	for _, part := range r.Parts {
		if part.ID != target {
			continue
		}
		if (part.Kind == "anchor" || part.Kind == "a") && part.Parent != "" && part.End-part.Start <= 16 {
			return part.Parent, true
		}
		return "", false
	}
	return "", false
}

func (m *model) noteLayout() {
	p := m.pager
	if p == nil || p.notes == nil {
		return
	}
	for _, f := range p.notes.frames {
		if f.doc != nil {
			at := reader.Position{}
			if len(f.layout.Lines) > 0 {
				at = f.layout.Lines[min(f.top, len(f.layout.Lines)-1)].Pos
			}
			width := m.pagerWidth()
			changed := f.layout.Width != width && len(f.layout.Lines) > 0
			f.layout.Update(f.doc, width)
			if changed {
				f.top = f.layout.LineFor(at)
			}
		}
	}
}

// followLink opens the chosen link as a new frame. Unresolved, missing, and
// outside targets are shown and never followed; nothing is fetched outside
// the book.
func (m *model) followLink() tea.Cmd {
	n := m.pager.notes
	if n.selected >= len(n.links) {
		return nil
	}
	link := n.links[n.selected]
	switch {
	case link.status != "resolved" || link.reference == "":
		n.notice = "Not followed: " + linkStatus(link) + "."
		return nil
	case len(n.frames) >= maxNoteDepth:
		n.notice = "Notes open " + strconv.Itoa(maxNoteDepth) + " deep at most. Escape returns one level."
		return nil
	}
	n.listing, n.notice = false, ""
	n.frames = append(n.frames, &noteFrame{link: link})
	return m.noteFetch("note", content.Request{Cursor: link.reference})
}

// closeNote returns one level: from the list to the frame or passage under
// it, or from a frame to the one under it. The passage itself never moved.
func (m *model) closeNote() {
	p := m.pager
	n := p.notes
	if n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
	// A late result for the closed level is ignored.
	n.request, n.fetching = m.nextSeq(), false
	switch {
	case n.listing:
		n.listing, n.segments, n.ends = false, nil, nil
	case len(n.frames) > 0:
		n.frames = n.frames[:len(n.frames)-1]
	}
	n.notice = ""
	if !n.active() {
		p.notice = "Back at the passage."
		if p.speed {
			p.notice = "Back at the passage, paused."
		}
	}
}

func linkStatus(l noteLink) string {
	switch l.status {
	case "resolved":
		if l.cyclic {
			return "in this book; it links back"
		}
		return "in this book"
	case "external", "external_not_fetched":
		return "outside the book; never fetched"
	case "missing_fragment":
		return "its target is missing from the book"
	case "outside_reading_order":
		return "its target is not in the reading order"
	}
	return present.Words(l.status)
}

func (m *model) noteKey(key string) tea.Cmd {
	p := m.pager
	n := p.notes
	switch key {
	case "esc", "backspace":
		m.closeNote()
		return tea.ClearScreen
	case "m", "'":
		n.notice = "Bookmarks save and restore the main passage. Escape returns to it first."
		return nil
	}
	if n.listing {
		switch key {
		case "down", "j":
			n.selected = min(len(n.links)-1, n.selected+1)
		case "up", "k":
			n.selected = max(0, n.selected-1)
		case "enter":
			return m.followLink()
		}
		return nil
	}
	f := n.top()
	if f == nil {
		return nil
	}
	switch key {
	case "f":
		return m.openLinks()
	case "space", "pgdown", "right", "l", "n":
		body := m.pagerBody() - 1
		switch {
		case f.top+body < len(f.layout.Lines):
			f.top += body
		case f.next != "" && !n.fetching:
			return m.noteFetch("more", content.Request{Cursor: f.next})
		case f.next == "" && f.failure == "":
			n.notice = "End of this note. Escape returns to the passage."
		}
	case "b", "pgup", "left", "h":
		f.top = max(0, f.top-(m.pagerBody()-1))
	case "down", "j":
		if f.top+1 < len(f.layout.Lines) {
			f.top++
		}
	case "up", "k":
		f.top = max(0, f.top-1)
	}
	return nil
}

// notesBody draws the link list or the note on screen.
func (m *model) notesBody(width int, margin string) (header string, body []string, notice string) {
	p := m.pager
	n := p.notes
	rows := m.pagerBody()
	if n.listing {
		header = "Links in this passage"
		if f := n.top(); f != nil {
			header = "Links in this note"
		}
		for i, l := range n.links {
			kind := "Link"
			if l.noteref {
				kind = "Note reference"
			}
			line := fmt.Sprintf("%s %d: %s (%s)", kind, i+1, l.label, linkStatus(l))
			marker := "  "
			if i == n.selected {
				marker = "> "
			}
			for j, row := range wrapRows(line, width-2) {
				if j > 0 {
					marker = "  "
				}
				body = append(body, margin+marker+row)
			}
		}
		notice = n.notice
		if notice == "" && len(n.links) > 0 {
			notice = "Enter follows the selected link. Escape returns."
		}
		return header, body[:min(len(body), rows)], notice
	}
	f := n.top()
	header = "Link target: " + f.link.label
	if f.link.noteref {
		header = "Note: " + f.link.label
	}
	if len(n.frames) > 1 {
		header += fmt.Sprintf(" (%d deep)", len(n.frames))
	}
	if f.note != "" {
		body = append(body, margin+m.paint(clip(f.note, width), muted, false))
	}
	if f.doc != nil {
		end := min(len(f.layout.Lines), f.top+rows-len(body))
		for _, line := range f.layout.Lines[min(f.top, end):end] {
			body = append(body, margin+m.noteLine(f, line))
		}
	} else if n.fetching {
		body = append(body, margin+"Retrieving the note.")
	}
	notice = n.notice
	if f.failure != "" {
		notice = f.failure
	}
	return header, body[:min(len(body), rows)], notice
}

func (m *model) noteLine(f *noteFrame, line reader.Line) string {
	switch line.Kind {
	case reader.Heading:
		return m.paint(f.layout.Text(line), accent, true)
	case reader.Gap:
		return m.paint(f.layout.Text(line), warning, false)
	}
	if line.Hang {
		return strings.Repeat(" ", reader.Indent) + f.layout.Text(line)
	}
	return f.layout.Text(line)
}

// wrapRows breaks one listing line at spaces into rows of at most width cells.
func wrapRows(s string, width int) []string {
	width = max(8, width)
	var rows []string
	row := ""
	for _, w := range strings.Fields(s) {
		switch {
		case row == "":
			row = w
		case reader.Width(row)+1+reader.Width(w) <= width:
			row += " " + w
		default:
			rows = append(rows, row)
			row = w
		}
	}
	return append(rows, clip(row, width))
}
