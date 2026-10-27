package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/cover"
	"github.com/blisspixel/nemalo/internal/present"
)

// Covers (decision 0105): a held EPUB's declared cover, decoded under the
// cover package's limits, drawn with Kitty graphics Unicode placeholders, or
// a title card of the same size. Covers are read only when a holding's
// details open, kept in memory for the session, and deleted from the
// terminal when the details close, the step changes, the window resizes and
// at exit.

const (
	coverCols, coverRows = 14, 10
	// coverMinWidth is the narrowest details pane with a cover column.
	coverMinWidth = 60
	coverCache    = 32
	// Image numbers are 256-colour foregrounds 16 to 255, so they never use
	// the basic colour codes.
	firstImage, lastImage = 16, 255
	cellAspect            = 2
)

type coverState struct {
	// protocol is "kitty" where images are drawn; off says why not.
	protocol, off string
	cache         map[string]*coverItem
	order         []string
	next          int
	// sent is the image number in the terminal and sentAsset its holding.
	sent      int
	sentAsset string
	// resized deletes and resends the image after a window resize.
	resized bool
}

type coverItem struct {
	status     string
	note       string
	thumb      *cover.Thumbnail
	cols, rows int
}

type coverLoaded struct {
	asset string
	item  *coverItem
}

// coverProtocol chooses the graphics protocol from the environment alone; it
// sends no query, so no reply can arrive as input.
func coverProtocol(getenv func(string) string, plain, off bool) (string, string) {
	term := getenv("TERM")
	switch {
	case plain:
		return "", "plain layout"
	case off || strings.EqualFold(getenv("NEMALO_COVERS"), "off"):
		return "", "covers are off"
	case getenv("TMUX") != "" || getenv("STY") != "" || strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux"):
		return "", "not drawn inside tmux or screen"
	case getenv("NO_COLOR") != "":
		return "", "not drawn without colour"
	case term == "xterm-kitty" || getenv("KITTY_WINDOW_ID") != "" || term == "xterm-ghostty" || getenv("TERM_PROGRAM") == "ghostty":
		return "kitty", ""
	}
	return "", "this terminal has no supported graphics protocol"
}

// coverEntry is the holding whose details show a cover column, if any.
func (m *model) coverEntry() (entry, bool) {
	if m.plain || !m.details || m.evidence || m.mode != "holdings" || len(m.entries) == 0 || m.preview.Width() < coverMinWidth {
		return entry{}, false
	}
	e := m.entries[m.selected]
	return e, e.managed && e.epub
}

// syncCover reads a missing cover and keeps the terminal's one image equal
// to the cover on screen. It runs after every update.
func (m *model) syncCover() tea.Cmd {
	e, ok := m.coverEntry()
	want := ""
	if ok {
		want = e.sourceID
	}
	var cmds []tea.Cmd
	resized := m.covers.resized
	m.covers.resized = false
	item := m.covers.cache[want]
	if ok && item == nil {
		cmds = append(cmds, m.loadCover(e))
		item = m.covers.cache[want]
		m.refreshPreview()
	}
	if m.covers.sent != 0 && (m.covers.sentAsset != want || item == nil || item.thumb == nil || resized) {
		cmds = append(cmds, tea.Raw(deleteImage(m.covers.sent)))
		m.covers.sent, m.covers.sentAsset = 0, ""
	}
	if ok && item != nil && item.thumb != nil && m.covers.sent == 0 {
		m.covers.next++
		if m.covers.next < firstImage || m.covers.next > lastImage {
			m.covers.next = firstImage
		}
		m.covers.sent, m.covers.sentAsset = m.covers.next, want
		cmds = append(cmds, tea.Raw(transmitImage(m.covers.sent, item)))
		m.refreshPreview()
	}
	return tea.Batch(cmds...)
}

// loadCover records a reading state, then reads and decodes off the update
// loop. Without a protocol nothing is read.
func (m *model) loadCover(e entry) tea.Cmd {
	if m.covers.cache == nil {
		m.covers.cache = map[string]*coverItem{}
	}
	if m.covers.protocol == "" {
		m.keepCover(e.sourceID, &coverItem{status: "off", note: "not shown: " + m.covers.off})
		return nil
	}
	m.keepCover(e.sourceID, &coverItem{status: "reading", note: "reading the declared cover"})
	library, asset, service, ctx := m.submittedInput, e.sourceID, m.service, m.ctx
	return func() tea.Msg {
		return coverLoaded{asset: asset, item: readCover(ctx, service.Cover, library, asset)}
	}
}

func readCover(ctx context.Context, read func(context.Context, string, string, string) (content.Cover, error), library, asset string) *coverItem {
	c, err := read(ctx, library, library, asset)
	switch {
	case err != nil:
		return &coverItem{status: "failed", note: "not shown: " + present.Words(firstOf(c.Status, "unavailable"))}
	case c.Status == "none":
		return &coverItem{status: "none", note: "none declared"}
	case c.Status == "remote":
		return &coverItem{status: "none", note: "declared remote image, not fetched"}
	case c.Status != "declared":
		return &coverItem{status: "none", note: "declaration names no manifest item"}
	}
	format := firstOf(cover.Format(c.Media), "unsupported")
	t, err := cover.Decode(c.Data, c.Media)
	if err != nil {
		note := "not shown: " + err.Error()
		if t.Width > 0 {
			note = fmt.Sprintf("%s %d x %d not shown: %s", format, t.Width, t.Height, err)
		}
		return &coverItem{status: "refused", note: note}
	}
	cols, rows := coverCells(t.Bounds.Dx(), t.Bounds.Dy())
	return &coverItem{status: "shown", note: fmt.Sprintf("declared %s, %d x %d", format, t.Width, t.Height), thumb: &t, cols: cols, rows: rows}
}

// keepCover caches an item, evicting the oldest beyond coverCache.
func (m *model) keepCover(asset string, item *coverItem) {
	if _, ok := m.covers.cache[asset]; !ok {
		m.covers.order = append(m.covers.order, asset)
	}
	m.covers.cache[asset] = item
	for len(m.covers.order) > coverCache {
		delete(m.covers.cache, m.covers.order[0])
		m.covers.order = m.covers.order[1:]
	}
}

func (m *model) coverLoaded(msg coverLoaded) {
	if m.covers.cache[msg.asset] == nil {
		return
	}
	m.keepCover(msg.asset, msg.item)
	m.refreshPreview()
}

// coverCells fits a w by h pixel image into the cover box, with cells twice
// as tall as wide.
func coverCells(w, h int) (int, int) {
	if w*coverRows*cellAspect > h*coverCols {
		return coverCols, max(1, min(coverRows, (coverCols*h+w*cellAspect/2)/(w*cellAspect)))
	}
	return max(1, min(coverCols, (coverRows*cellAspect*w+h/2)/h)), coverRows
}

// transmitImage sends the thumbnail once, directly and quietly, with a
// virtual placement for Unicode placeholders.
func transmitImage(id int, item *coverItem) string {
	payload := base64.StdEncoding.EncodeToString(item.thumb.PNG)
	var b strings.Builder
	for first := true; first || payload != ""; first = false {
		chunk := payload[:min(len(payload), kitty.MaxChunkSize)]
		payload = payload[len(chunk):]
		more := "0"
		if payload != "" {
			more = "1"
		}
		opts := []string{"q=2", "m=" + more}
		if first {
			opts = []string{"a=T", "f=100", "t=d", fmt.Sprintf("i=%d", id), "U=1", fmt.Sprintf("c=%d", item.cols), fmt.Sprintf("r=%d", item.rows), "q=2", "m=" + more}
		}
		b.WriteString(ansi.KittyGraphics([]byte(chunk), opts...))
	}
	return b.String()
}

// deleteImage frees an image and its placements in the terminal.
func deleteImage(id int) string {
	return ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", id), "q=2")
}

// coverBox is the cover column: coverRows rows of coverCols cells, the image
// placeholders where it is shown and the title card otherwise.
func (m *model) coverBox(e entry, item *coverItem) []string {
	if item != nil && item.thumb != nil && m.covers.sent != 0 && m.covers.sentAsset == e.sourceID {
		rows := make([]string, coverRows)
		left := (coverCols - item.cols) / 2
		for r := range coverRows {
			if r >= item.rows {
				rows[r] = strings.Repeat(" ", coverCols)
				continue
			}
			var b strings.Builder
			b.WriteString(strings.Repeat(" ", left))
			fmt.Fprintf(&b, "\x1b[38;5;%dm", m.covers.sent)
			for c := range item.cols {
				b.WriteRune(kitty.Placeholder)
				b.WriteRune(kitty.Diacritic(r))
				b.WriteRune(kitty.Diacritic(c))
			}
			b.WriteString("\x1b[39m" + strings.Repeat(" ", coverCols-left-item.cols))
			rows[r] = b.String()
		}
		return rows
	}
	return m.titleCard(e)
}

// titleCard is a rounded box of the cover's size with the wrapped title and
// the format.
func (m *model) titleCard(e entry) []string {
	inner := coverCols - 2
	lines := strings.Split(lipgloss.Wrap(e.title, inner, " "), "\n")
	lines = lines[:min(len(lines), coverRows-4)]
	for len(lines) < coverRows-3 {
		lines = append(lines, "")
	}
	lines = append(lines, "EPUB")
	rows := []string{m.paint("╭"+strings.Repeat("─", inner)+"╮", muted, false)}
	for _, l := range lines {
		l = clip(l, inner)
		rows = append(rows, m.paint("│", muted, false)+l+strings.Repeat(" ", inner-lipgloss.Width(l))+m.paint("│", muted, false))
	}
	return append(rows, m.paint("╰"+strings.Repeat("─", inner)+"╯", muted, false))
}

// withCover lays the cover column beside the wrapped card text and adds the
// cover outcome after it.
func (m *model) withCover(e entry, width int) string {
	item := m.covers.cache[e.sourceID]
	note := "reading the declared cover"
	if item != nil {
		note = item.note
	}
	textWidth := max(1, width-coverCols-2)
	text := strings.Split(lipgloss.Wrap(e.detail+"\nCover  "+note, textWidth, ""), "\n")
	box := m.coverBox(e, item)
	var out []string
	for i := range max(len(box), len(text)) {
		left := strings.Repeat(" ", coverCols)
		if i < len(box) {
			left = box[i]
		}
		right := ""
		if i < len(text) {
			right = text[i]
		}
		out = append(out, left+"  "+right)
	}
	return strings.Join(out, "\n")
}

// releaseCover is the escape sequence that frees the image at exit, if any.
func (m *model) releaseCover() string {
	if m.covers.sent == 0 {
		return ""
	}
	id := m.covers.sent
	m.covers.sent, m.covers.sentAsset = 0, ""
	return deleteImage(id)
}
