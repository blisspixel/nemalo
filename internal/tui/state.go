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
	m.confirmKind, m.confirmRoot, m.confirmSource, m.confirmReport, m.confirmStatus = "init", root, "", m.report, m.status
	m.input.Blur()
	m.secondary.Blur()
	m.status = "Review initialization. Enter confirms; Escape returns without writing."
	m.setReport(fmt.Sprintf("Initialize library control state\n\nExplicit root: %q\n\nCreates only .nemalo/writer.lock and .nemalo/journal.jsonl inside this existing folder.\n\nExisting content is preserved. This does not import, assess, organize, open, or delete books.\n\nValid interrupted initialization can resume. Corrupt or unexpected state remains for review. Directory-entry power-loss durability depends on the filesystem.\n\nEnter initializes this exact root. Escape returns without writing.\n", root))
	m.viewport.GotoTop()
	m.resize()
}

func (m *model) requestImport() {
	if m.input.Value() == "" || m.secondary.Value() == "" {
		m.status = "Enter the library folder and a source file or intake packet."
		m.resize()
		return
	}
	root, err := filepath.Abs(m.input.Value())
	if err != nil {
		m.status = fmt.Sprintf("Import error: %q", err.Error())
		m.resize()
		return
	}
	source, err := filepath.Abs(m.secondary.Value())
	if err != nil {
		m.status = fmt.Sprintf("Import error: %q", err.Error())
		m.resize()
		return
	}
	m.confirmKind, m.confirmRoot, m.confirmSource, m.confirmReport, m.confirmStatus = "import", root, source, m.report, m.status
	m.input.Blur()
	m.secondary.Blur()
	m.status = "Review import. Enter confirms; Escape returns without writing."
	m.setReport(fmt.Sprintf("Import one file into this library\n\nLibrary: %q\nSource: %q\n\nCopies one assessed EPUB, PDF, or MP3, or the single content file in a completed intake packet. The source stays in place. Nothing is deleted.\n\nThis review does not run antivirus. A new result stays in review. An asset already recorded as checked keeps that status.\n\nEnter imports this exact source. Escape, F7, or F8 returns without a new write.\n", root, source))
	m.viewport.GotoTop()
	m.resize()
}

func (m *model) confirmInitialization(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc", "f7", "f8":
		m.setReport(m.confirmReport)
		m.status = m.confirmStatus
		m.confirmKind, m.confirmRoot, m.confirmSource, m.confirmReport, m.confirmStatus = "", "", "", "", ""
		m.resize()
		return m.focusForm()
	case "pgup", "pgdown":
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return cmd
	case "enter":
		kind := m.confirmKind
		m.input.SetValue(m.confirmRoot)
		if kind == "import" {
			m.secondary.SetValue(m.confirmSource)
		}
		m.confirmKind, m.confirmRoot, m.confirmSource, m.confirmReport, m.confirmStatus = "", "", "", "", ""
		if kind == "import" {
			m.importAction = true
		} else {
			m.initializeAction = true
		}
		return m.start(true)
	}
	return nil
}
