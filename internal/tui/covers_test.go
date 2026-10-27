package tui

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/library"
)

func coverPNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func coverEPUB(t *testing.T, item, image string) []byte {
	return epubBytes(t, map[string]string{
		"META-INF/container.xml": `<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="book.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"book.opf":               `<package xmlns="http://www.idpf.org/2007/opf"><metadata><title>Cover Book</title><language>en</language></metadata><manifest><item id="u" href="unit.xhtml" media-type="application/xhtml+xml"/>` + item + `</manifest><spine><itemref idref="u"/></spine></package>`,
		"unit.xhtml":             "<html><body><p>text</p></body></html>",
		"art.png":                image,
	}, []string{"META-INF/container.xml", "book.opf", "unit.xhtml", "art.png"})
}

// coverModel holds each book in a new library and lists them in Holdings.
func coverModel(t *testing.T, books ...[]byte) (*model, []string) {
	t.Helper()
	ctx := context.Background()
	lib := t.TempDir()
	if _, err := library.Initialize(ctx, lib); err != nil {
		t.Fatal(err)
	}
	m := newModel(ctx, app.Service{})
	m.switchMode("holdings")
	m.submittedInput = lib
	m.covers.protocol = "kitty"
	var page library.ManagedPage
	var ids []string
	for i, data := range books {
		got := holdTUIBook(t, ctx, lib, fmt.Sprintf("book%d.epub", i), data)
		page.Holdings = append(page.Holdings, library.Holding{ID: got.AssetID, Format: "epub", Titles: []string{"Cover Book"}, Path: got.Path, Bytes: int64(len(data))})
		ids = append(ids, got.AssetID)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(result{id: m.id, data: page})
	m.Update(key(tea.KeyF6))
	return m, ids
}

// collect runs a command as the program would and returns the raw
// terminal writes it asked for.
func collect(m *model, cmd tea.Cmd) string {
	var raw strings.Builder
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				walk(c)
			}
		case tea.RawMsg:
			raw.WriteString(fmt.Sprint(msg.Msg))
		case coverLoaded:
			_, next := m.Update(msg)
			walk(next)
		}
	}
	walk(cmd)
	return raw.String()
}

func TestCoverProtocolComesFromTheEnvironmentOnly(t *testing.T) {
	for _, tc := range []struct {
		env       map[string]string
		plain     bool
		off       bool
		protocol  string
		offReason string
	}{
		{env: map[string]string{"TERM": "xterm-kitty"}, protocol: "kitty"},
		{env: map[string]string{"TERM": "xterm-256color", "KITTY_WINDOW_ID": "1"}, protocol: "kitty"},
		{env: map[string]string{"TERM": "xterm-ghostty"}, protocol: "kitty"},
		{env: map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "ghostty"}, protocol: "kitty"},
		{env: map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "WezTerm"}, offReason: "no supported graphics"},
		{env: map[string]string{"TERM": "foot"}, offReason: "no supported graphics"},
		{env: map[string]string{"TERM": "xterm-kitty", "TMUX": "/tmp/tmux"}, offReason: "tmux"},
		{env: map[string]string{"TERM": "screen-256color", "KITTY_WINDOW_ID": "1"}, offReason: "tmux"},
		{env: map[string]string{"TERM": "xterm-kitty", "STY": "1.pts"}, offReason: "screen"},
		{env: map[string]string{"TERM": "xterm-kitty", "NO_COLOR": "1"}, offReason: "colour"},
		{env: map[string]string{"TERM": "xterm-kitty", "NEMALO_COVERS": "OFF"}, offReason: "covers are off"},
		{env: map[string]string{"TERM": "xterm-kitty"}, off: true, offReason: "covers are off"},
		{env: map[string]string{"TERM": "xterm-kitty"}, plain: true, offReason: "plain"},
	} {
		protocol, off := coverProtocol(func(k string) string { return tc.env[k] }, tc.plain, tc.off)
		if protocol != tc.protocol || !strings.Contains(off, tc.offReason) || (protocol == "") == (off == "") {
			t.Errorf("%v: %q %q", tc.env, protocol, off)
		}
	}
	m := newModel(context.Background(), app.Service{})
	m.covers.protocol = "kitty"
	m.Update(tea.ColorProfileMsg{})
	if m.covers.protocol != "" || !strings.Contains(m.covers.off, "256") {
		t.Fatal("covers drawn without 256 colours", m.covers)
	}
}

func TestCoverCellsKeepTheAspectInsideTheBox(t *testing.T) {
	for _, tc := range [][4]int{{280, 400, 14, 10}, {280, 140, 14, 4}, {50, 400, 3, 10}, {400, 1, 14, 1}, {1, 400, 1, 10}} {
		if cols, rows := coverCells(tc[0], tc[1]); cols != tc[2] || rows != tc[3] {
			t.Errorf("%v: %d x %d", tc, cols, rows)
		}
	}
}

// Details read and send the declared cover once, draw placeholder cells in
// the same box the title card uses, and delete the image when the details
// close, on resize (sending it again) and at exit.
func TestCoverIsSentShownAndDeleted(t *testing.T) {
	m, ids := coverModel(t, coverEPUB(t, `<item id="k" href="art.png" media-type="image/png" properties="cover-image"/>`, coverPNG(t, 300, 450)))
	_, cmd := m.Update(key(tea.KeyEnter))
	if !m.details || m.covers.cache[ids[0]] == nil || m.covers.cache[ids[0]].status != "reading" {
		t.Fatal("details did not start reading the cover")
	}
	card := m.preview.GetContent()
	raw := collect(m, cmd)
	if !strings.HasPrefix(raw, "\x1b_Ga=T,f=100,t=d,i=16,U=1,c=13,r=10,q=2,") || strings.Contains(raw, "t=f") || strings.Contains(raw, "t=t") {
		t.Fatalf("transmission %.80q", raw)
	}
	shown := m.preview.GetContent()
	if !strings.Contains(shown, string(kitty.Placeholder)) || !strings.Contains(shown, "\x1b[38;5;16m") || !strings.Contains(shown, "declared PNG, 300 x 450") {
		t.Fatalf("no placeholder cells:\n%s", shown)
	}
	if lipgloss.Height(card) != lipgloss.Height(shown) || strings.Count(card, "\n") != strings.Count(shown, "\n") {
		t.Fatal("the title card and the cover differ in size")
	}
	for i, line := range strings.Split(shown, "\n")[:coverRows] {
		if w := lipgloss.Width(line[:strings.Index(line+"  ", "  ")]); i < coverRows && lipgloss.Width(strings.Split(card, "\n")[i]) != lipgloss.Width(line) && w > coverCols {
			t.Fatalf("row %d moved", i)
		}
	}
	// The terminal frame keeps the cells, and nothing overflows.
	if v := m.View().Content; !strings.Contains(v, string(kitty.Placeholder)) || lipgloss.Width(v) > 100 || lipgloss.Height(v) > 30 {
		t.Fatal("frame lost the cover")
	}
	_, cmd = m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	if raw := collect(m, cmd); !strings.HasPrefix(raw, "\x1b_Ga=d,d=I,i=16,q=2\x1b\\") || !strings.Contains(raw, "a=T,f=100,t=d,i=17,") {
		t.Fatalf("resize %.80q", raw)
	}
	_, cmd = m.Update(key(tea.KeyEnter))
	if raw := collect(m, cmd); raw != "\x1b_Ga=d,d=I,i=17,q=2\x1b\\" || m.covers.sent != 0 || strings.Contains(m.preview.GetContent(), string(kitty.Placeholder)) {
		t.Fatalf("closing %q", raw)
	}
	// Reopening uses the session cache and reads nothing.
	_, cmd = m.Update(key(tea.KeyEnter))
	if raw := collect(m, cmd); !strings.Contains(raw, "i=18,") || m.covers.cache[ids[0]].status != "shown" {
		t.Fatal("cached cover not sent")
	}
	if m.releaseCover() != "\x1b_Ga=d,d=I,i=18,q=2\x1b\\" || m.releaseCover() != "" {
		t.Fatal("exit left the image")
	}
	m.Update(key(tea.KeyEnter))
	_, cmd = m.Update(key(tea.KeyEnter))
	collect(m, cmd)
	_, cmd = m.Update(key(tea.KeyTab))
	if raw := collect(m, cmd); !strings.Contains(raw, "a=d,d=I,i=19,") || m.covers.sent != 0 {
		t.Fatalf("step change %q", raw)
	}
}

// Refused or absent covers keep the title card and say why; a terminal
// without a protocol reads nothing at all.
func TestCoverRefusalsKeepTheTitleCard(t *testing.T) {
	bomb := []byte(coverPNG(t, 1, 1))
	// Rewrite the IHDR to 5000 by 5000; the decoder refuses it from the header.
	copy(bomb[16:24], []byte{0, 0, 0x13, 0x88, 0, 0, 0x13, 0x88})
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	for name, tc := range map[string]struct {
		book []byte
		note string
	}{
		"none":     {coverEPUB(t, `<item id="k" href="art.png" media-type="image/png"/>`, coverPNG(t, 4, 4)), "Cover  none declared"},
		"mismatch": {coverEPUB(t, `<item id="k" href="art.png" media-type="image/jpeg" properties="cover-image"/>`, coverPNG(t, 4, 4)), "do not match"},
		"svg":      {coverEPUB(t, `<item id="k" href="art.png" media-type="image/svg+xml" properties="cover-image"/>`, "<svg/>"), "not JPEG, PNG or GIF"},
		"bomb":     {coverEPUB(t, `<item id="k" href="art.png" media-type="image/png" properties="cover-image"/>`, string(bomb)), "dimensions exceed"},
		"remote":   {coverEPUB(t, `<item id="k" href="https://example.invalid/c.png" media-type="image/png" properties="cover-image"/>`, ""), "not fetched"},
	} {
		m, _ := coverModel(t, tc.book)
		_, cmd := m.Update(key(tea.KeyEnter))
		if raw := collect(m, cmd); raw != "" {
			t.Fatalf("%s sent %q", name, raw)
		}
		got := m.preview.GetContent()
		if strings.Contains(got, string(kitty.Placeholder)) || !strings.Contains(got, "╭") || !strings.Contains(got, tc.note) {
			t.Fatalf("%s:\n%s", name, got)
		}
	}
	m, ids := coverModel(t, coverEPUB(t, `<item id="k" href="art.png" media-type="image/png" properties="cover-image"/>`, coverPNG(t, 4, 4)))
	m.covers.protocol, m.covers.off = "", "this terminal has no supported graphics protocol"
	_, cmd := m.Update(key(tea.KeyEnter))
	if cmd != nil && collect(m, cmd) != "" || m.covers.cache[ids[0]].status != "off" || !strings.Contains(m.preview.GetContent(), "Cover  not shown: this terminal") {
		t.Fatal("unsupported terminal read or drew a cover")
	}
	// Narrow panes and plain mode show neither the cover nor the card.
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	if strings.Contains(m.preview.GetContent(), "╭") {
		t.Fatal("cover column below 60 columns")
	}
	m.plain = true
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	if strings.Contains(m.preview.GetContent(), "╭") || strings.Contains(m.preview.GetContent(), "Cover  ") {
		t.Fatal("plain mode drew a cover column")
	}
}

// Moving through 500 holdings reads no cover; only opening details does.
func TestScrollingHoldingsReadsNoCover(t *testing.T) {
	m, _ := coverModel(t)
	m.covers.protocol = "kitty"
	page := library.ManagedPage{Total: 500}
	for i := range 500 {
		page.Holdings = append(page.Holdings, library.Holding{ID: fmt.Sprintf("sha256:%064x", i), Format: "epub", Titles: []string{fmt.Sprint("Book ", i)}})
	}
	m.Update(result{id: m.id, data: page})
	for range 499 {
		if _, cmd := m.Update(key(tea.KeyDown)); cmd != nil && collect(m, cmd) != "" {
			t.Fatal("scrolling sent an image")
		}
	}
	if m.selected != 499 || len(m.covers.cache) != 0 {
		t.Fatal("scrolling read covers", m.selected, len(m.covers.cache))
	}
	_, cmd := m.Update(key(tea.KeyEnter))
	collect(m, cmd)
	if item := m.covers.cache[page.Holdings[499].ID]; item == nil || item.status != "failed" || !strings.Contains(m.preview.GetContent(), "╭") {
		t.Fatal("a missing holding did not fall back to the title card")
	}
	// The cache keeps at most coverCache covers.
	for i := range coverCache + 5 {
		m.keepCover(fmt.Sprint(i), &coverItem{status: "none"})
	}
	if len(m.covers.cache) != coverCache || len(m.covers.order) != coverCache {
		t.Fatal("cover cache grew", len(m.covers.cache))
	}
}

type frameModel string

func (f frameModel) Init() tea.Cmd                       { return nil }
func (f frameModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return f, tea.Quit }
func (f frameModel) View() tea.View                      { return tea.NewView(string(f)) }

// The program's cell renderer writes the placeholder cells, their
// diacritics and their 256-colour image number unchanged.
func TestRendererKeepsPlaceholderCells(t *testing.T) {
	m, _ := coverModel(t, coverEPUB(t, `<item id="k" href="art.png" media-type="image/png" properties="cover-image"/>`, coverPNG(t, 280, 400)))
	_, cmd := m.Update(key(tea.KeyEnter))
	collect(m, cmd)
	box := strings.Join(m.coverBox(m.entries[0], m.covers.cache[m.entries[0].sourceID]), "\n")
	var out bytes.Buffer
	p := tea.NewProgram(frameModel(box), tea.WithOutput(&out), tea.WithInput(nil), tea.WithWindowSize(40, 12), tea.WithColorProfile(colorprofile.ANSI256))
	go p.Send(tea.KeyPressMsg{Code: 'q'})
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	for r := range coverRows {
		cell := string([]rune{kitty.Placeholder, kitty.Diacritic(r), kitty.Diacritic(0)})
		if !strings.Contains(out.String(), cell) {
			t.Fatalf("row %d placeholder missing from %q", r, out.String())
		}
	}
	if !strings.Contains(out.String(), "38;5;16m") {
		t.Fatal("image number colour lost")
	}
}
