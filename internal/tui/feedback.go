package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/present"
)

// Feedback (decision 0104): elapsed time and phase while work runs, a bar
// only where the service counts bytes, outcome-specific completion text,
// short toasts and a session message log. Reduced motion (plain mode,
// TERM=dumb or NEMALO_REDUCE_MOTION) draws no spinner and no toasts, and
// changes the busy line at most every five seconds.

// tick drives the spinner and elapsed time while work runs, and expires
// toasts; it never starts work.
type tick struct{}

const (
	motionInterval = 125 * time.Millisecond
	idleInterval   = time.Second
	toastLife      = 5 * time.Second
	logLimit       = 500
)

var spinnerFrames = []string{"|", "/", "-", "\\"}

// progressState is what a running service reported last. Services call
// set from their own goroutine; the terminal reads it on each tick.
type progressState struct {
	mu          sync.Mutex
	phase       string
	done, total int64
}

func (p *progressState) set(phase string, done, total int64) {
	p.mu.Lock()
	p.phase, p.done, p.total = phase, done, total
	p.mu.Unlock()
}

func (p *progressState) get() (string, int64, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.phase, p.done, p.total
}

type logEntry struct {
	at      time.Time
	text    string
	outcome outcome
}

type toast struct {
	text    string
	outcome outcome
	at      time.Time
}

func tickAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tick{} })
}

// onTick advances the spinner and expires a toast, then schedules the next
// tick: quickly while work runs with motion, otherwise once a second.
func (m *model) onTick() tea.Cmd {
	if m.busy {
		m.spin++
	}
	if t := m.toast; t != nil && t.outcome != outcomeFailed && m.now().Sub(t.at) >= toastLife {
		m.toast = nil
	}
	if m.busy && !m.reduceMotion {
		return tickAfter(motionInterval)
	}
	return tickAfter(idleInterval)
}

// afterUpdate records what a message changed: when work started and ended,
// a toast for a finished result, and each new status in the session log.
func (m *model) afterUpdate(wasBusy bool, message tea.Msg) {
	switch {
	case m.busy && !wasBusy:
		m.started = m.now()
	case !m.busy && wasBusy:
		m.started, m.progress = time.Time{}, nil
		if _, ok := message.(result); ok && !m.reduceMotion {
			m.toast = &toast{text: outcomeWord(m.outcome) + ": " + m.status, outcome: m.outcome, at: m.now()}
		}
	}
	if m.status != "" && m.status != m.lastLogged {
		m.lastLogged = m.status
		m.messages = append(m.messages, logEntry{at: m.now(), text: m.status, outcome: m.outcome})
		if len(m.messages) > logLimit {
			m.messages = m.messages[len(m.messages)-logLimit:]
		}
	}
}

func outcomeWord(o outcome) string {
	switch o {
	case outcomeFailed:
		return "Failed"
	case outcomeAttention:
		return "Needs attention"
	}
	return "Done"
}

// elapsed is how long work has run. Reduced motion rounds down to five
// seconds so the line changes rarely.
func (m *model) elapsed() string {
	if m.started.IsZero() {
		return ""
	}
	d := m.now().Sub(m.started)
	if m.reduceMotion {
		d = d.Truncate(5 * time.Second)
	}
	s := int(d.Seconds())
	if s >= 60 {
		return fmt.Sprintf("%dm %02ds", s/60, s%60)
	}
	return fmt.Sprintf("%ds", s)
}

// busyLabel is the running work's state for the panel title.
func (m *model) busyLabel() string {
	label := "working"
	if !m.reduceMotion {
		label = spinnerFrames[m.spin%len(spinnerFrames)] + " " + label
	}
	if e := m.elapsed(); e != "" {
		label += " " + e
	}
	return label
}

// busyLines describe running work: its phase, a bar when the service counts
// bytes, and how to stop it. The bar stays below 100% until a result says
// the work succeeded.
func (m *model) busyLines(width int) []string {
	lines := []string{m.status}
	if m.progress != nil {
		phase, done, total := m.progress.get()
		if phase != "" {
			lines = append(lines, "Phase: "+present.Words(phase))
		}
		if total > 0 {
			lines = append(lines, progressBar(done, total, max(10, min(40, width-30)))+fmt.Sprintf("  %s of %s", byteSize(done), byteSize(total)))
		} else if done > 0 {
			lines = append(lines, byteSize(done)+" so far; the total is not known")
		}
	}
	if e := m.elapsed(); e != "" {
		lines = append(lines, "Elapsed "+e+".")
	}
	lines = append(lines, m.paint("Escape cancels; Ctrl+C quits.", muted, false))
	return lines
}

func progressBar(done, total int64, width int) string {
	percent := min(99, int(done*100/max(1, total)))
	filled := width * percent / 100
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + fmt.Sprintf("] %d%%", percent)
}

// trackProgress gives a request a progress callback for this run.
func (m *model) trackProgress() func(string, int64, int64) {
	p := &progressState{}
	m.progress = p
	return p.set
}

// footerWithToast puts a toast at the right of the footer, dropping keys
// when the row is too narrow for both.
func (m *model) footerWithToast(footer string) string {
	if m.toast == nil || m.reduceMotion || strings.Contains(footer, "\n") {
		return footer
	}
	role := success
	switch m.toast.outcome {
	case outcomeFailed:
		role = danger
	case outcomeAttention:
		role = warning
	}
	width := max(1, m.width-2)
	hint := ""
	if m.toast.outcome == outcomeFailed {
		hint = "  (any key dismisses)"
	}
	// The message is clipped at a word; the dismiss hint stays whole.
	shown := func(room int) string { return clipWords(m.toast.text, max(1, room-lipgloss.Width(hint))) + hint }
	room := width - lipgloss.Width(footer) - 3
	if room >= 20+lipgloss.Width(hint) {
		text := shown(room)
		return footer + strings.Repeat(" ", max(3, width-lipgloss.Width(footer)-lipgloss.Width(text))) + m.paint(text, role, false)
	}
	return m.paint(shown(width), role, false)
}

// logLines render the session's messages, oldest first, from top.
func (m *model) logLines(width, height int) []string {
	if len(m.messages) == 0 {
		return []string{"No messages yet in this session."}
	}
	var rows []string
	for _, e := range m.messages {
		row := e.at.Format("15:04:05") + "  " + outcomeWord(e.outcome) + "  " + e.text
		rows = append(rows, strings.Split(lipgloss.Wrap(row, max(1, width), " "), "\n")...)
	}
	m.logTop = max(0, min(m.logTop, len(rows)-height))
	return rows[m.logTop:min(len(rows), m.logTop+height)]
}

func (m *model) logKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "L", "q":
		m.logOpen = false
		m.resize()
	case "up":
		m.logTop = max(0, m.logTop-1)
	case "down":
		m.logTop++
	case "pgup":
		m.logTop = max(0, m.logTop-10)
	case "pgdown":
		m.logTop += 10
	case "ctrl+c":
		m.logOpen = false
		if m.cancel != nil {
			m.cancel()
		}
		return tea.Quit
	}
	return nil
}

func (m *model) openLog() {
	m.logOpen = true
	m.logTop = max(0, len(m.messages)*2)
	m.resize()
}

// summary is the outcome of a finished result in words, from its typed
// fields; it never claims more than the result does.
func summary(data any) string {
	switch v := data.(type) {
	case library.ImportResult:
		if v.Applied {
			title := present.Exact(v.Path)
			if len(v.Titles) > 0 {
				title = present.Line(v.Titles[0])
			}
			return "Imported " + title + " as " + present.Words(firstOf(v.Status, "held")) + reasonSuffix(v.Status, v.Reason) + "."
		}
	case library.IngestResult:
		if v.Applied {
			return fmt.Sprintf("Ingested: %d imported, %d not imported, %d failed.", v.Imported, v.NotImported, v.Failed)
		}
	case library.RescanResult:
		if v.Applied {
			return fmt.Sprintf("Rescanned %d: %d promoted, %d with detections, %d incomplete.", len(v.Scanned), v.Promoted, v.Detections, v.Incomplete)
		}
	case library.CleanupResult:
		if v.Applied {
			return fmt.Sprintf("Moved %d files to the holding area.", v.Moved)
		}
	case library.PurgeResult:
		if v.Applied {
			return fmt.Sprintf("Purged %d held copies; the library keeps its own.", v.Purged)
		}
	case library.RestoreResult:
		if v.Applied {
			return fmt.Sprintf("Restored %d files.", len(v.Files))
		}
	case assessment.Report:
		text := "Checks: " + strings.ToLower(healthChip(v.Status).text) + "; " + strings.ToLower(antivirusChip(v.Antivirus.Status).text)
		if v.Antivirus.Status == "no_detections_reported" {
			text += " (not proof of safety)"
		}
		return text + "."
	case inventory.Report:
		return fmt.Sprintf("Inventoried %d entries; %s.", len(v.Entries), map[bool]string{true: "complete", false: "incomplete"}[v.Complete])
	case library.Snapshot:
		return fmt.Sprintf("Saved a snapshot of %d assets.", len(v.Catalog.Assets))
	case library.Audit:
		return fmt.Sprintf("Audit: %d unchanged locations; %s.", v.Unchanged, map[bool]string{true: "complete", false: "incomplete"}[v.Complete])
	case library.LibraryCheck:
		state := "needs attention"
		if v.Healthy {
			state = "no problems found"
		}
		return fmt.Sprintf("Library check: %s; %d of %d holdings verified.", state, v.Verified, v.Holdings)
	case library.State:
		return "Library status: " + present.Words(v.Status) + "."
	case discovery.Page:
		return fmt.Sprintf("%d results of %d; discovery metadata only.", len(v.Results), v.Total)
	case discovery.Evaluation:
		if v.OpenAccess != nil {
			return fmt.Sprintf("Evaluated %s: %d locations stated by OpenAlex; discovery only.", present.Line(v.ID), len(v.OpenAccess.Locations))
		}
		return fmt.Sprintf("Evaluated %s: %d files listed; declarations only.", present.Line(v.ID), len(v.Files))
	case library.Page:
		return fmt.Sprintf("%d matching assets.", v.Total)
	case library.ManagedPage:
		return fmt.Sprintf("%d matching holdings.", v.Total)
	case library.CollectionStatusResult:
		return fmt.Sprintf("Collection status: %d held, %d not held; offline.", v.Held, v.NotHeld)
	case acquisition.Result, acquisition.RecordingResult, app.CollectionAcquireResult:
		// Their own status lines below describe scanning and intake.
	}
	return "Complete."
}

func reasonSuffix(status, reason string) string {
	if reason == "" || reason == status {
		return ""
	}
	return " (" + present.Words(reason) + ")"
}
