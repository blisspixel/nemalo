// Package tui presents shared operations in a keyboard-driven terminal interface.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/present"
)

type result struct {
	id   int
	data any
	err  error
}

type model struct {
	ctx              context.Context
	service          app.Service
	input            textinput.Model
	secondary        textinput.Model
	secondaryFocused bool
	assess           bool
	height           int
	width            int
	color            bool
	dark             bool
	largeBudget      bool
	drafts           map[string]draft
	offset           int
	total            int
	submittedInput   string
	submittedSecond  string
	viewport         viewport.Model
	report           string
	preview          viewport.Model
	entries          []entry
	selected         int
	resultsFocused   bool
	details          bool
	evidence         bool
	mode             string
	source           string
	format           string
	status           string
	busy             bool
	id               int
	cancel           context.CancelFunc
}

func newModel(ctx context.Context, service app.Service) *model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 1000
	input.Placeholder = "Search books (sent to Open Library on Enter)"
	input.SetWidth(70)
	input.SetVirtualCursor(true)
	input.SetStyles(textinput.Styles{Cursor: textinput.CursorStyle{Blink: true}})
	secondary := textinput.New()
	secondary.Prompt = ""
	secondary.CharLimit = 1000
	secondary.SetWidth(70)
	secondary.SetVirtualCursor(true)
	secondary.SetStyles(textinput.Styles{Cursor: textinput.CursorStyle{Blink: true}})
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(16))
	view.SoftWrap = true
	m := &model{ctx: ctx, service: service, input: input, secondary: secondary, width: 80, height: 25, dark: true, viewport: view, mode: "search", source: "openlibrary", format: "epub", drafts: map[string]draft{}, status: "Ready. Network activity only on explicit search/evaluation."}
	m.preview = viewport.New()
	m.preview.SoftWrap = true
	m.resize()
	return m
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.input.Focus(), tea.RequestBackgroundColor) }

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
	case tea.ColorProfileMsg:
		profile := msg.Profile.String()
		m.color = (profile == "ANSI" || profile == "ANSI256" || profile == "TrueColor") && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = msg.Height
		m.input.SetWidth(max(1, msg.Width-4))
		m.secondary.SetWidth(max(1, msg.Width-4))
		m.viewport.SetWidth(max(1, msg.Width-2))
		m.resize()
	case result:
		if msg.id != m.id {
			return m, nil
		}
		m.busy = false
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.status = "Complete."
		switch value := msg.data.(type) {
		case discovery.Page:
			if msg.err == nil {
				m.offset, m.total = value.Offset, value.Total
			}
		case library.Page:
			if msg.err == nil {
				m.offset, m.total = value.Offset, value.Total
			}
		case library.Snapshot:
			if msg.err == nil {
				m.seedLibraryForms(value.Output, value.Catalog.RootHint)
			}
		}
		if msg.err != nil {
			m.status = fmt.Sprintf("Incomplete: %q", msg.err.Error())
		}
		m.populate(msg.data)
		if msg.err != nil {
			m.populate(nil)
			m.evidence = true
		}
		data, err := json.MarshalIndent(msg.data, "", "  ")
		switch value := msg.data.(type) {
		case discovery.Evaluation:
			data = []byte(present.Evaluation(value))
		case library.Snapshot:
			data = []byte(present.Snapshot(value))
		case library.Page:
			data = []byte(present.Holdings(value))
		case library.Audit:
			data = []byte(present.Audit(value))
		case assessment.Report:
			data = []byte(present.Health(value))
		case discovery.Page:
			data = []byte(present.Search(value))
		case inventory.Report:
			data = []byte(present.Inventory(value))
		}
		if err != nil {
			m.status = fmt.Sprintf("Output error: %q", err.Error())
			m.setReport("")
		} else {
			if msg.err != nil {
				data = append([]byte(fmt.Sprintf("Operation error: %q\n\n", msg.err.Error())), data...)
			}
			m.setReport(string(data))
			m.viewport.GotoTop()
		}
		m.resize()
	case tea.KeyPressMsg:
		if handled, cmd := m.browserKey(msg.String()); handled {
			return m, cmd
		}
		if strings.HasPrefix(msg.String(), "alt+") {
			for i, name := range modes {
				if msg.String() == fmt.Sprintf("alt+%d", i+1) && !m.busy {
					return m, m.switchMode(name)
				}
			}
		}
		switch msg.String() {
		case "f4":
			if m.mode == "holdings" && !m.busy {
				formats := []string{"epub", "pdf", "mp3", "all"}
				for i, format := range formats {
					if format == m.format {
						m.format = formats[(i+1)%len(formats)]
						break
					}
				}
				m.offset, m.total = 0, 0
				m.setReport("")
				m.populate(nil)
				m.status = "Format filter changed. Press Enter to load holdings."
				m.resize()
			}
			return m, nil
		case "f3":
			if !m.busy && (m.mode == "inspect" || m.mode == "snapshot" || m.mode == "audit") {
				m.largeBudget = !m.largeBudget
			}
			return m, nil
		case "ctrl+right", "ctrl+left":
			return m, m.turnPage(msg.String() == "ctrl+right")
		case "f2":
			if m.mode == "search" && !m.busy {
				if m.source == "openlibrary" {
					m.source = "archive"
				} else {
					m.source = "openlibrary"
				}
				m.input.Placeholder = "Search " + m.source + " (network on Enter; F2 changes provider)"
				m.status = "Ready. Search provider: " + m.source
				m.offset, m.total = 0, 0
				m.setReport("")
				m.populate(nil)
				m.resize()
			}
			return m, nil
		case "ctrl+n":
			if !m.form() || m.busy {
				return m, nil
			}
			m.secondaryFocused = !m.secondaryFocused
			m.resultsFocused = false
			m.resize()
			if m.secondaryFocused {
				m.input.Blur()
				return m, m.secondary.Focus()
			}
			m.secondary.Blur()
			return m, m.input.Focus()
		case "ctrl+a":
			if m.mode == "snapshot" && !m.busy {
				m.assess = !m.assess
				return m, nil
			}
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "esc":
			if m.cancel != nil {
				m.cancel()
				m.cancel = nil
			}
			m.id++
			m.busy = false
			m.status = "Cancelled. Sources unchanged."
			m.resize()
			return m, nil
		case "tab", "shift+tab":
			if m.busy {
				return m, nil
			}
			step := 1
			if msg.String() == "shift+tab" {
				step = -1
			}
			return m, m.switchMode(modes[(m.modeIndex()+step+len(modes))%len(modes)])
		case "enter":
			return m, m.start(true)
		case "pgup", "pgdown":
			var cmd tea.Cmd
			if !m.evidence && len(m.entries) > 0 && (m.details || m.width >= 100) {
				m.preview, cmd = m.preview.Update(msg)
			} else {
				m.viewport, cmd = m.viewport.Update(msg)
			}
			return m, cmd
		}
	}
	if m.busy {
		return m, nil
	}
	if m.resultsFocused {
		return m, nil
	}
	var cmd tea.Cmd
	if m.secondaryFocused {
		m.secondary, cmd = m.secondary.Update(message)
	} else {
		m.input, cmd = m.input.Update(message)
	}
	return m, cmd
}

func (m *model) form() bool { return m.mode == "snapshot" || m.mode == "holdings" || m.mode == "audit" }

func Run(ctx context.Context, service app.Service, in io.Reader, out io.Writer) error {
	m := newModel(ctx, service)
	defer func() {
		if m.cancel != nil {
			m.cancel()
		}
	}()
	_, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}
