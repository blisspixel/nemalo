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
	"github.com/blisspixel/nemalo/internal/present"
)

type result struct {
	id   int
	data any
	err  error
}

type model struct {
	ctx      context.Context
	service  app.Service
	input    textinput.Model
	viewport viewport.Model
	mode     string
	status   string
	busy     bool
	id       int
	cancel   context.CancelFunc
}

func newModel(ctx context.Context, service app.Service) *model {
	input := textinput.New()
	input.CharLimit = 1000
	input.Placeholder = "Search books (sent to Open Library on Enter)"
	input.SetWidth(70)
	input.SetVirtualCursor(true)
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(16))
	view.SoftWrap = true
	return &model{ctx: ctx, service: service, input: input, viewport: view, mode: "search", status: "Ready. No network activity until you submit a search."}
}

func (m *model) Init() tea.Cmd { return m.input.Focus() }

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.input.SetWidth(max(1, msg.Width-4))
		m.viewport.SetWidth(max(1, msg.Width-2))
		m.viewport.SetHeight(max(1, msg.Height-9))
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
			if m.mode == "search" {
				m.mode = "inspect"
				m.input.Placeholder = "Explicit folder path (read-only inventory)"
			} else if m.mode == "inspect" {
				m.mode = "check"
				m.input.Placeholder = "Explicit file path (health checks; no antivirus)"
			} else if m.mode == "check" {
				m.mode = "scan"
				m.input.Placeholder = "File path (health + antivirus; scanner cloud/sample policy applies)"
			} else {
				m.mode = "search"
				m.input.Placeholder = "Search books (sent to Open Library on Enter)"
			}
			m.status = "Ready."
			return m, nil
		case "enter":
			if m.busy || m.input.Value() == "" {
				return m, nil
			}
			ctx, cancel := context.WithCancel(m.ctx)
			m.cancel = cancel
			m.id++
			id := m.id
			mode, value := m.mode, m.input.Value()
			m.busy = true
			m.status = "Working. Escape cancels; Ctrl+C quits."
			return m, func() tea.Msg {
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
	m.input, cmd = m.input.Update(message)
	return m, cmd
}

func (m *model) View() tea.View {
	v := tea.NewView("Nemalo\nFind knowledge. Care for it. Put it to work.\n\nMode: " + m.mode + " (Tab: search / inspect / check / scan)\n" + m.input.View() + "\n" + m.status + "\n\n" + m.viewport.View() + "\nEnter: run  Escape: cancel  PageUp/PageDown: scroll  Ctrl+C: quit")
	v.AltScreen = true
	return v
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
