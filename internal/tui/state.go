package tui

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

func (m *model) requestInitialization() {
	if m.input.Value() == "" {
		m.status = "Enter an existing library folder first."
		m.resize()
		return
	}
	root, err := filepath.Abs(m.input.Value())
	if err != nil {
		m.status = fmt.Sprintf("Initialization error: %q", err.Error())
		m.resize()
		return
	}
	m.confirmRoot, m.confirmReport, m.confirmStatus = root, m.report, m.status
	m.input.Blur()
	m.secondary.Blur()
	m.status = "Review initialization. Enter confirms; Escape returns without writing."
	m.setReport(fmt.Sprintf("Initialize library control state\n\nExplicit root: %q\n\nCreates only .nemalo/writer.lock and .nemalo/journal.jsonl inside this existing folder.\n\nExisting content is preserved. This does not import, assess, organize, open, or delete books.\n\nValid interrupted initialization can resume. Corrupt or unexpected state remains for review. Directory-entry power-loss durability depends on the filesystem.\n\nEnter initializes this exact root. Escape returns without writing.\n", root))
	m.viewport.GotoTop()
	m.resize()
}

func (m *model) confirmInitialization(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc", "f7":
		m.setReport(m.confirmReport)
		m.status = m.confirmStatus
		m.confirmRoot, m.confirmReport, m.confirmStatus = "", "", ""
		m.resize()
		return m.focusForm()
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return cmd
	case "enter":
		m.input.SetValue(m.confirmRoot)
		m.confirmRoot, m.confirmReport, m.confirmStatus = "", "", ""
		m.initializeAction = true
		return m.start(true)
	}
	return nil
}
