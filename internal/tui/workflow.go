package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/inventory"
)

var modes = []string{"search", "inspect", "check", "scan", "snapshot", "holdings", "audit", "evaluate", "state"}

type draft struct {
	input, second, content, status  string
	submittedInput, submittedSecond string
	offset, total                   int
	entries                         []entry
	selected                        int
	details, evidence               bool
}

func (m *model) modeIndex() int {
	for i, name := range modes {
		if name == m.mode {
			return i
		}
	}
	return 0
}

func (m *model) switchMode(name string) tea.Cmd {
	if name == m.mode {
		return nil
	}
	m.drafts[m.mode] = draft{input: m.input.Value(), second: m.secondary.Value(), content: m.report, status: m.status, submittedInput: m.submittedInput, submittedSecond: m.submittedSecond, offset: m.offset, total: m.total, entries: m.entries, selected: m.selected, details: m.details, evidence: m.evidence}
	m.mode = name
	d := m.drafts[name]
	m.input.SetValue(d.input)
	m.secondary.SetValue(d.second)
	m.secondary.Blur()
	m.secondaryFocused = false
	m.offset, m.total = d.offset, d.total
	m.submittedInput, m.submittedSecond = d.submittedInput, d.submittedSecond
	m.status = d.status
	if m.status == "" {
		m.status = "Ready."
	}
	m.setReport(d.content)
	m.entries, m.details, m.evidence = d.entries, d.details, d.evidence
	m.resultsFocused = false
	m.selectEntry(d.selected)
	m.viewport.GotoTop()
	m.resize()
	return m.input.Focus()
}

func (m *model) seedLibraryForms(catalog, root string) {
	for _, mode := range []string{"holdings", "audit"} {
		d := m.drafts[mode]
		if mode == m.mode {
			continue
		}
		if d.input == "" {
			d.input = catalog
			if mode == "audit" {
				d.second = root
			}
			m.drafts[mode] = d
		}
	}
}

func (m *model) turnPage(forward bool) tea.Cmd {
	if m.busy || (m.mode != "search" && m.mode != "holdings") {
		return nil
	}
	if m.input.Value() != m.submittedInput || m.secondary.Value() != m.submittedSecond {
		m.status = "Inputs changed. Press Enter to start a new query."
		return nil
	}
	size, ceiling := 10, 10000
	if m.mode == "holdings" {
		size, ceiling = 50, 100000
	}
	next := max(0, m.offset-size)
	if forward {
		next = m.offset + size
	}
	if next == m.offset || next > ceiling || next >= m.total {
		return nil
	}
	m.offset = next
	return m.start(false)
}

func (m *model) start(reset bool) tea.Cmd {
	if m.busy || m.input.Value() == "" {
		return nil
	}
	if (m.mode == "snapshot" || m.mode == "audit") && m.secondary.Value() == "" {
		m.status = "Enter the required second field with Ctrl+N."
		return nil
	}
	if reset {
		m.offset, m.total = 0, 0
	}
	m.submittedInput, m.submittedSecond = m.input.Value(), m.secondary.Value()
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.id++
	id, mode, value, source := m.id, m.mode, m.input.Value(), m.source
	if mode == "state" && m.initializeAction {
		mode = "initialize"
	}
	m.initializeAction = false
	second, assess, offset, format := m.secondary.Value(), m.assess, m.offset, m.format
	limits := inventory.Defaults()
	if m.largeBudget {
		limits.TotalBytes = 5 << 30
	}
	m.busy = true
	m.status = "Working. Escape cancels; Ctrl+C quits."
	m.setReport("")
	m.populate(nil)
	m.resize()
	return func() tea.Msg {
		switch mode {
		case "initialize":
			data, err := m.service.InitializeLibrary(ctx, value)
			return result{id, data, err}
		case "state":
			data, err := m.service.LibraryStatus(ctx, value)
			return result{id, data, err}
		case "evaluate":
			data, err := m.service.Evaluate(ctx, value)
			return result{id, data, err}
		case "snapshot":
			data, err := m.service.Snapshot(ctx, value, second, limits, assess)
			return result{id, data, err}
		case "holdings":
			data, err := m.service.HoldingsFormat(value, second, format, 50, offset)
			return result{id, data, err}
		case "audit":
			data, err := m.service.Audit(ctx, value, second, limits)
			return result{id, data, err}
		case "search":
			data, err := m.service.SearchSource(ctx, source, value, 10, offset)
			return result{id, data, err}
		case "check", "scan":
			data, err := m.service.Check(ctx, value, assessment.Options{Scan: mode == "scan"})
			return result{id, data, err}
		default:
			data, err := m.service.Inspect(ctx, value, limits)
			return result{id, data, err}
		}
	}
}
