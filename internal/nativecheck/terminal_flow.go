package nativecheck

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/pdftext/pdftest"
)

type TerminalReport struct {
	Schema    string        `json:"schema"`
	Status    string        `json:"status"`
	Profile   string        `json:"profile"`
	PDFSHA256 string        `json:"pdf_sha256"`
	Setup     []Command     `json:"setup"`
	Runs      []TerminalRun `json:"runs"`
}
type TerminalRun struct {
	Name            string         `json:"name"`
	Status          string         `json:"status"`
	Backend         string         `json:"backend"`
	Args            []string       `json:"args"`
	Raw             string         `json:"raw"`
	OutputBytes     int            `json:"output_bytes"`
	InputBytes      int64          `json:"input_bytes"`
	Exit            int            `json:"exit"`
	Cases           []TerminalCase `json:"cases"`
	LastScreen      string         `json:"last_screen,omitempty"`
	ScreenTruncated bool           `json:"screen_truncated,omitempty"`
}
type TerminalPosition struct {
	Unit int `json:"unit"`
	Byte int `json:"byte"`
}
type TerminalCase struct {
	ID       string            `json:"id"`
	Status   string            `json:"status"`
	Frame    string            `json:"frame"`
	SHA256   string            `json:"sha256"`
	Width    int               `json:"width"`
	Height   int               `json:"height"`
	Position *TerminalPosition `json:"position,omitempty"`
	Cursor   *TerminalCursor   `json:"cursor,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// TerminalCursor is the real terminal cursor in the observed frame.
type TerminalCursor struct {
	X      int  `json:"x"`
	Y      int  `json:"y"`
	Hidden bool `json:"hidden"`
}

var terminalCoordinate = regexp.MustCompile(`Unit ([0-9]+), byte ([0-9]+)`)

var readerTerminalCases = []string{"pages", "find-open", "find-results", "find-selected", "contents", "contents-selected", "speed-paused", "playing", "help", "help-dwell", "help-return", "page-context", "word-return", "narrow", "wide", "exit"}
var referenceTerminalCases = []string{"startup", "field-cursor", "holdings", "read-staged", "no-excerpt-refusal", "units", "excerpt", "citation", "citation-sidecar", "citation-return", "evidence", "evidence-sidecar", "replay", "evidence-end", "replay-key", "narrow", "wide", "evidence-return", "continuation", "actions", "actions-citation", "actions-return", "exit"}

func terminalPlace(text string) *TerminalPosition {
	m := terminalCoordinate.FindStringSubmatch(text)
	if len(m) != 3 {
		return nil
	}
	u, err := strconv.Atoi(m[1])
	if err != nil {
		return nil
	}
	b, err := strconv.Atoi(m[2])
	if err != nil {
		return nil
	}
	return &TerminalPosition{u, b}
}
func atTerminalPlace(f terminalScreen, p *TerminalPosition) bool {
	got := terminalPlace(f.text)
	return got != nil && p != nil && *got == *p
}
func containsScreen(text ...string) func(terminalScreen) bool {
	return func(f terminalScreen) bool {
		for _, s := range text {
			if !strings.Contains(f.text, s) {
				return false
			}
		}
		return true
	}
}

type terminalDriver struct {
	suite      *suite
	session    *terminalSession
	report     *TerminalRun
	directory  string
	frameBytes int
	endPlace   *TerminalPosition
}

func (s *suite) terminals() {
	r := &TerminalReport{Schema: "nemalo-terminal-check/1", Status: "failed", Profile: "xterm-256color; NO_COLOR=1", Setup: []Command{}, Runs: []TerminalRun{}}
	s.result.Terminal = r
	s.mkdir("terminal")
	s.mkdir("terminal-pdf-library")
	s.mkdir("terminal-pdf-source")
	pdf := pdftest.Pages("", "A synthetic PDF quotation. "+strings.Repeat("alpha beta gamma delta epsilon ", 200), "Second synthetic page. "+strings.Repeat("alpha beta gamma delta epsilon ", 200))
	digest := sha256.Sum256(pdf)
	r.PDFSHA256 = hex.EncodeToString(digest[:])
	s.write("terminal-pdf-source/book.pdf", pdf)
	lib, pdfLib := s.path("library"), s.path("terminal-pdf-library")
	var plan, held library.ImportResult
	s.callInto(&r.Setup, "terminal-pdf-init", 0, nil, "library", "init", pdfLib)
	s.callInto(&r.Setup, "terminal-pdf-import-preview", 0, &plan, "library", "import", pdfLib, s.path("terminal-pdf-source/book.pdf"))
	s.callInto(&r.Setup, "terminal-pdf-import-apply", 0, &held, "library", "import", pdfLib, s.path("terminal-pdf-source/book.pdf"), "--apply", "--plan", plan.Plan)
	s.require(held.Applied && held.Status == "review" && held.Reason == "unscanned" && held.SHA256 == r.PDFSHA256, "terminal PDF setup did not retain exact unscanned bytes")
	if !s.guard() {
		return
	}
	before := map[string]map[string]fileFact{}
	for _, name := range []string{"library", "source", "intake", "config", "state", "cache", "terminal-pdf-library", "terminal-pdf-source"} {
		before[name] = s.snapshot(name)
	}
	asset := "sha256:" + s.result.FixtureSHA256["book"]
	s.terminalRun("epub-reader", []string{"read", lib, "--root", lib, "--asset", asset}, func(d *terminalDriver) { d.reader(false) })
	s.terminalRun("pdf-reader", []string{"read", pdfLib, "--root", pdfLib, "--asset", held.AssetID}, func(d *terminalDriver) { d.reader(true) })
	s.terminalRun("tui-reference", []string{"tui"}, func(d *terminalDriver) { d.reference(lib, asset) })
	for name, facts := range before {
		s.unchanged(name, facts)
	}
	if s.guard() {
		r.Status = "passed"
	}
}

func (s *suite) terminalRun(name string, args []string, flow func(*terminalDriver)) {
	if !s.guard() {
		return
	}
	directory := "terminal/" + name
	s.mkdir(directory)
	if !s.guard() {
		return
	}
	in, err := s.checkedInvocation(args)
	s.fail(err)
	if !s.guard() {
		return
	}
	in.env = terminalEnvironment(s.directory)
	limit := min(terminalOutputLimit, (8<<20)-s.outputBytes-terminalFrameLimit)
	r := TerminalRun{Name: name, Status: "failed", Args: args, Exit: -1, Raw: directory + "/raw.txt", Cases: []TerminalCase{}}
	if runtime.GOOS == "windows" {
		r.Backend = "windows-conpty"
	} else {
		r.Backend = "unix-pty"
	}
	s.result.Terminal.Runs = append(s.result.Terminal.Runs, r)
	index := len(s.result.Terminal.Runs) - 1
	session, err := newTerminalSession(s.ctx, in, limit, startTerminal)
	if err != nil {
		s.fail(fmt.Errorf("%s: %w", name, err))
		return
	}
	d := &terminalDriver{suite: s, session: session, report: &r, directory: directory}
	flow(d)
	if s.err == nil {
		s.fail(session.waitExit())
	}
	raw, read, stopErr := session.stop()
	s.fail(stopErr)
	if s.err == nil {
		f := session.screen()
		var exitErr error
		if f.alt {
			exitErr = errors.New("terminal exit did not restore the primary screen")
		}
		if d.endPlace != nil {
			// Only generated ASCII closing instructions may wrap across cells.
			// Source quotations and evidence frames retain their exact text.
			compact := strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, f.text)
			for _, want := range []string{"Nothingaboutthisreadingwasrecorded.", fmt.Sprintf("--unit%d", d.endPlace.Unit), fmt.Sprintf("--text-offset%d", d.endPlace.Byte)} {
				if !strings.Contains(compact, want) {
					exitErr = errors.Join(exitErr, fmt.Errorf("reader closing screen omitted %q", want))
				}
			}
		}
		d.save("exit", f, exitErr)
		want := readerTerminalCases
		if name == "tui-reference" {
			want = referenceTerminalCases
		}
		got := make([]string, len(r.Cases))
		for i, c := range r.Cases {
			got[i] = c.ID
		}
		s.require(slices.Equal(got, want), fmt.Sprintf("%s terminal case sequence is incomplete: %v", name, got))
	}
	r.OutputBytes, r.InputBytes = read, session.inputBytes.Load()
	select {
	case <-session.exitReady:
		r.Exit = session.exit.code
	default:
	}
	s.outputBytes += read
	s.fail(s.root.WriteFile(r.Raw, raw, 0600)) // Failure diagnostics outlive cancellation.
	if s.outputBytes > 8<<20 {
		s.fail(errors.New("terminal diagnostics exceeded the shared allowance"))
	}
	if s.err == nil && r.Exit == 0 {
		r.Status = "passed"
	}
	s.result.Terminal.Runs[index] = r
}

func (d *terminalDriver) save(id string, f terminalScreen, err error) terminalScreen {
	if id == "" || len(id) > 80 || strings.Trim(id, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		d.suite.fail(errors.New("invalid terminal case identity"))
		return f
	}
	data := []byte(f.text)
	if len(data) > 64<<10 {
		data = data[:64<<10]
		err = errors.Join(err, errors.New("terminal frame exceeded its allowance"))
	}
	d.frameBytes += len(data)
	if d.frameBytes > terminalFrameLimit {
		err = errors.Join(err, errors.New("saved terminal frames exceeded their allowance"))
	}
	d.suite.outputBytes += len(data)
	file := d.directory + "/" + id + ".txt"
	digest := sha256.Sum256(data)
	c := TerminalCase{ID: id, Status: "passed", Frame: file, SHA256: hex.EncodeToString(digest[:]), Width: f.width, Height: f.height, Position: terminalPlace(f.text), Cursor: &TerminalCursor{X: f.cursorX, Y: f.cursorY, Hidden: f.cursorHidden}}
	if err != nil {
		c.Status = "failed"
		c.Error = err.Error()
		d.report.LastScreen = f.text[:min(len(f.text), 16<<10)]
		d.report.ScreenTruncated = len(f.text) > 16<<10
	}
	d.report.Cases = append(d.report.Cases, c)
	d.suite.fail(errors.Join(err, d.suite.root.WriteFile(file, data, 0600)))
	return f
}
func (d *terminalDriver) observe(id string, after uint64, predicate func(terminalScreen) bool) terminalScreen {
	if !d.suite.guard() {
		return terminalScreen{}
	}
	f, err := d.session.waitScreen(after, predicate)
	return d.save(id, f, err)
}
func (d *terminalDriver) key(id, text string, predicate func(terminalScreen) bool) terminalScreen {
	if !d.suite.guard() {
		return terminalScreen{}
	}
	before, err := d.session.send(text)
	if err != nil {
		return d.save(id, d.session.screen(), err)
	}
	return d.observe(id, before, predicate)
}
func (d *terminalDriver) send(text string) {
	if d.suite.guard() {
		_, err := d.session.send(text)
		d.suite.fail(err)
	}
}

func (d *terminalDriver) reader(pdf bool) {
	text, query, choice := "Knowledge is power.", "Knowledge is power.", "Synthetic chapter"
	if pdf {
		text, query, choice = "A synthetic PDF quotation.", "A synthetic PDF quotation.", "Physical page 2"
	}
	d.observe("pages", 0, containsScreen("Nemalo reader", text))
	d.key("find-open", "/", containsScreen("Find exact text in the whole book"))
	d.send(query)
	d.key("find-results", "\r", containsScreen("Query:", "Unit 0, bytes"))
	d.key("find-selected", "\r", containsScreen("Nemalo reader", text, "Opened an exact whole-book match"))
	d.key("contents", "c", containsScreen("Contents and source pages", choice))
	if pdf {
		d.send("j")
	}
	d.key("contents-selected", "\r", func(f terminalScreen) bool {
		if strings.Contains(f.text, "Contents and source pages") {
			return false
		}
		if pdf {
			p := terminalPlace(f.text)
			return p != nil && p.Unit == 1 && strings.Contains(f.text, "Second synthetic page.")
		}
		return strings.Contains(f.text, "Knowledge is power.")
	})
	d.key("speed-paused", "s", containsScreen("250 wpm nominal, paused"))
	d.key("playing", " ", func(f terminalScreen) bool {
		p := terminalPlace(f.text)
		return p != nil && p.Byte >= 10 && strings.Contains(f.text, "playing")
	})
	help := d.key("help", "?", containsScreen("Reader keys", "hide this list"))
	at := terminalPlace(help.text)
	if !d.suite.guard() {
		return
	}
	if at == nil {
		d.suite.fail(errors.New("reader help omitted its exact source place"))
		return
	}
	timer := time.NewTimer(1200 * time.Millisecond)
	defer timer.Stop()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	dwell := help
loop:
	for {
		select {
		case <-d.session.ctx.Done():
			d.suite.fail(d.session.ctx.Err())
			return
		case <-timer.C:
			break loop
		case <-tick.C:
			dwell = d.session.screen()
			if !atTerminalPlace(dwell, at) || !strings.Contains(dwell.text, "Reader keys") {
				d.save("help-dwell", dwell, errors.New("help changed or hid its source place during dwell"))
				return
			}
		}
	}
	dwell = d.session.screen()
	if !atTerminalPlace(dwell, at) || !strings.Contains(dwell.text, "Reader keys") {
		d.save("help-dwell", dwell, errors.New("help changed or hid its source place at the end of dwell"))
		return
	}
	d.save("help-dwell", dwell, nil)
	d.endPlace = at
	d.key("help-return", "?", func(f terminalScreen) bool {
		return atTerminalPlace(f, at) && strings.Contains(f.text, "250 wpm nominal, paused") && !strings.Contains(f.text, "Reader keys")
	})
	d.key("page-context", "p", func(f terminalScreen) bool {
		return !strings.Contains(f.text, "wpm nominal") && strings.Contains(f.text, "Nemalo reader")
	})
	d.key("word-return", "s", func(f terminalScreen) bool {
		return atTerminalPlace(f, at) && strings.Contains(f.text, "250 wpm nominal, paused")
	})
	for _, size := range []struct {
		id   string
		w, h int
	}{{"narrow", 40, 24}, {"wide", 116, 36}} {
		if !d.suite.guard() {
			return
		}
		before, err := d.session.resize(size.w, size.h)
		if err != nil {
			d.save(size.id, d.session.screen(), err)
			return
		}
		d.observe(size.id, before, func(f terminalScreen) bool {
			return f.width == size.w && f.height == size.h && len(strings.Split(f.text, "\n")) == size.h && atTerminalPlace(f, at) && strings.Contains(f.text, "paused")
		})
	}
	d.send("q")
}

func (d *terminalDriver) reference(lib, asset string) {
	d.observe("startup", 0, containsScreen("Nemalo", "[Discover]", "[Search]"))
	d.send("\x1b6") // xterm Alt+6: Holdings.
	d.send(lib)
	d.send("\x0e") // Ctrl+N changes the field; Tab changes modes.
	d.send(asset)
	// The real terminal cursor follows the typed text on the focused field.
	d.observe("field-cursor", 0, func(f terminalScreen) bool {
		lines := strings.Split(f.text, "\n")
		if f.cursorHidden || f.cursorY < 0 || f.cursorY >= len(lines) {
			return false
		}
		// A value wider than its field scrolls: the cursor follows the
		// longest visible tail of the typed text.
		line := lines[f.cursorY]
		if !strings.Contains(line, "> Filter") {
			return false
		}
		for k := len(asset); k >= 8; k-- {
			if at := strings.Index(line, asset[len(asset)-k:]); at >= 0 {
				return f.cursorX == utf8.RuneCountInString(line[:at])+k
			}
		}
		return false
	})
	d.key("holdings", "\r", containsScreen("Native synthetic fixture"))
	d.send("\x1b[17~") // F6, results focus.
	d.key("read-staged", "r", containsScreen("[Read]", "Asset "+asset[:19]))
	d.key("no-excerpt-refusal", "\x1bOS", containsScreen("successful excerpt"))
	d.key("units", "\r", containsScreen("Source units available"))
	d.send("\x1b[17~")
	ordinary := d.key("excerpt", "\r", containsScreen("Bounded range delivered", "Knowledge is power."))
	d.key("citation", "\x1bOQ", containsScreen("Nemalo | Recorded citation metadata", "Unknown fields"))
	d.key("citation-sidecar", "\x1b[F", containsScreen(`"author"`, "Esc back"))
	d.key("citation-return", "\x1b", func(f terminalScreen) bool { return f.text == ordinary.text })
	d.key("evidence", "\x1bOS", containsScreen("Nemalo | Source-bound evidence", "Metadata SHA-256", "F3/r replay"))
	d.key("evidence-sidecar", "\x1b[F", containsScreen(`"has_continuation"`, "F3/r replay"))
	d.key("replay", "\x1bOR", containsScreen("Nemalo | Source-bound evidence", "Metadata SHA-256", "F3/r replay"))
	// An identical screen sends nothing, so scroll first; r then returns to the top.
	d.key("evidence-end", "\x1b[F", containsScreen(`"has_continuation"`, "F3/r replay"))
	d.key("replay-key", "r", containsScreen("Nemalo | Source-bound evidence", "Metadata SHA-256", "F3/r replay"))
	for _, size := range []struct {
		id   string
		w, h int
	}{{"narrow", 40, 24}, {"wide", 116, 36}} {
		if !d.suite.guard() {
			return
		}
		before, err := d.session.resize(size.w, size.h)
		if err != nil {
			d.save(size.id, d.session.screen(), err)
			return
		}
		d.observe(size.id, before, func(f terminalScreen) bool {
			return f.width == size.w && f.height == size.h && len(strings.Split(f.text, "\n")) == size.h && strings.Contains(f.text, "Source-bound evidence") && strings.Contains(f.text, "Esc back")
		})
	}
	d.key("evidence-return", "\x1b", containsScreen("Bounded range delivered", "Knowledge is power."))
	d.key("continuation", "n", containsScreen("Bounded range delivered", "bytes 4096..8192"))
	// Ctrl+O reaches a function-key action by name, without a function key.
	d.key("actions", "\x0f", containsScreen("Actions", "Find:", "Show recorded citation metadata"))
	d.send("citation")
	d.key("actions-citation", "\r", containsScreen("Nemalo | Recorded citation metadata", "Unknown fields"))
	d.key("actions-return", "\x1b", containsScreen("Bounded range delivered", "bytes 4096..8192"))
	d.send("\x03")
}
