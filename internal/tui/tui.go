// Package tui presents shared operations in a keyboard-driven terminal interface.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

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
	viewport         viewport.Model
	mode             string
	status           string
	busy             bool
	id               int
	cancel           context.CancelFunc
}

func newModel(ctx context.Context, service app.Service) *model {
	input := textinput.New()
	input.CharLimit = 1000
	input.Placeholder = "Search books (sent to Open Library on Enter)"
	input.SetWidth(70)
	input.SetVirtualCursor(true)
	secondary := textinput.New()
	secondary.CharLimit = 1000
	secondary.SetWidth(70)
	secondary.SetVirtualCursor(true)
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(16))
	view.SoftWrap = true
	return &model{ctx: ctx, service: service, input: input, secondary: secondary, height: 25, viewport: view, mode: "search", status: "Ready. No network activity until you submit a search."}
}

func (m *model) Init() tea.Cmd { return m.input.Focus() }

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
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
		if msg.err != nil {
			m.status = fmt.Sprintf("Incomplete: %q", msg.err.Error())
		}
		data, err := json.MarshalIndent(msg.data, "", "  ")
		switch value := msg.data.(type) {
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
			m.viewport.SetContent("")
		} else {
			m.viewport.SetContent(string(data))
			m.viewport.GotoTop()
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+n":
			if !m.form() || m.busy {
				return m, nil
			}
			m.secondaryFocused = !m.secondaryFocused
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
			return m, nil
		case "tab":
			if m.busy {
				return m, nil
			}
			m.input.Reset()
			m.secondary.Reset()
			m.secondary.Blur()
			m.secondaryFocused = false
			if m.mode == "search" {
				m.mode = "inspect"
				m.input.Placeholder = "Explicit folder path (read-only inventory)"
			} else if m.mode == "inspect" {
				m.mode = "check"
				m.input.Placeholder = "Explicit file path (health checks; no antivirus)"
			} else if m.mode == "check" {
				m.mode = "scan"
				m.input.Placeholder = "File path (health + antivirus; scanner cloud/sample policy applies)"
			} else if m.mode == "scan" {
				m.mode = "snapshot"
				m.input.Placeholder = "Folder to catalog (sources unchanged)"
				m.secondary.Placeholder = "New catalog output path outside that folder"
			} else if m.mode == "snapshot" {
				m.mode = "holdings"
				m.input.Placeholder = "Catalog file"
				m.secondary.Placeholder = "Optional literal filter: title, language, filename, or hash"
			} else if m.mode == "holdings" {
				m.mode = "audit"
				m.input.Placeholder = "Catalog file"
				m.secondary.Placeholder = "Explicit root directory to audit (required)"
			} else {
				m.mode = "search"
				m.input.Placeholder = "Search books (sent to Open Library on Enter)"
			}
			m.status = "Ready."
			m.resize()
			return m, m.input.Focus()
		case "enter":
			if m.busy || m.input.Value() == "" {
				return m, nil
			}
			if (m.mode == "snapshot" || m.mode == "audit") && m.secondary.Value() == "" {
				m.status = "Enter the required second field with Ctrl+N."
				return m, nil
			}
			ctx, cancel := context.WithCancel(m.ctx)
			m.cancel = cancel
			m.id++
			id := m.id
			mode, value := m.mode, m.input.Value()
			second, assess := m.secondary.Value(), m.assess
			m.busy = true
			m.status = "Working. Escape cancels; Ctrl+C quits."
			return m, func() tea.Msg {
				switch mode {
				case "snapshot":
					data, err := m.service.Snapshot(ctx, value, second, inventory.Defaults(), assess)
					return result{id, data, err}
				case "holdings":
					data, err := m.service.Holdings(value, second, 50, 0)
					return result{id, data, err}
				case "audit":
					data, err := m.service.Audit(ctx, value, second, inventory.Defaults())
					return result{id, data, err}
				}
				if mode == "search" {
					data, err := m.service.Search(ctx, value, 10, 0)
					return result{id, data, err}
				}
				if mode == "check" || mode == "scan" {
					data, err := m.service.Check(ctx, value, assessment.Options{Scan: mode == "scan"})
					return result{id, data, err}
				}
				data, err := m.service.Inspect(ctx, value, inventory.Defaults())
				return result{id, data, err}
			}
		case "pgup", "pgdown", "ctrl+u", "ctrl+d":
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}
	var cmd tea.Cmd
	if m.secondaryFocused {
		m.secondary, cmd = m.secondary.Update(message)
	} else {
		m.input, cmd = m.input.Update(message)
	}
	return m, cmd
}

func (m *model) View() tea.View {
	form := ""
	if m.form() {
		form = "\n" + m.secondary.View() + "\nCtrl+N: switch fields"
	}
	if m.mode == "snapshot" {
		form += fmt.Sprintf("; Ctrl+A: local health metadata (%t)", m.assess)
	}
	v := tea.NewView("Nemalo\nFind knowledge. Care for it. Put it to work.\n\nMode: " + m.mode + " (Tab: next mode)\n" + m.input.View() + form + "\n" + m.status + "\n\n" + m.viewport.View() + "\nEnter: run  Escape: cancel  PageUp/PageDown: scroll  Ctrl+C: quit")
	v.AltScreen = true
	return v
}

func (m *model) form() bool { return m.mode == "snapshot" || m.mode == "holdings" || m.mode == "audit" }
func (m *model) resize() {
	extra := 0
	if m.form() {
		extra = 2
	}
	m.viewport.SetHeight(max(1, m.height-9-extra))
}

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
