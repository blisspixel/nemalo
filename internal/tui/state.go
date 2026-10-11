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
	m.setReport(fmt.Sprintf("Import one file into this library\n\nLibrary: %q\nSource: %q\n\nCopies one assessed EPUB, PDF, or MP3, or the single content file in a completed intake packet. A plain folder is ingested: every supported file, packet, and ZIP member in it is imported. Sources stay in place. Nothing is deleted.\n\nThis review does not run antivirus. A new result stays in review. An asset already recorded as checked keeps that status.\n\nEnter imports this exact source. Escape, F7, or F8 returns without a new write.\n", root, source))
	m.viewport.GotoTop()
	m.resize()
}

// requestRescan reviews scanning the library's review holdings that lack
// usable scan evidence. Nothing runs until a second Enter.
func (m *model) requestRescan() {
	if m.input.Value() == "" {
		m.status = "Enter an initialized library folder first."
		m.resize()
		return
	}
	root, err := filepath.Abs(m.input.Value())
	if err != nil {
		m.status = fmt.Sprintf("Rescan error: %q", err.Error())
		m.resize()
		return
	}
	m.confirmKind, m.confirmRoot, m.confirmSource, m.confirmReport, m.confirmStatus = "rescan", root, "", m.report, m.status
	m.input.Blur()
	m.secondary.Blur()
	m.status = "Review rescan. Enter scans and records results; Escape returns without scanning."
	m.setReport(fmt.Sprintf("Rescan review holdings\n\nLibrary: %q\n\nRuns the limited checks again and scans, with the installed scanner, each EPUB holding that is in review only because no usable scan was recorded. ClamAV scans them in batches so its signatures load once.\n\nA clean result records the holding as checked; a detection or an incomplete scan keeps it in review with that reason. PDF, MP3, earlier detections, and other findings are listed but not scanned.\n\nStored files are never changed, moved, or deleted. A no-detection result is not proof of safety.\n\nEnter scans and records the results. Escape, F7, F8, or Ctrl+R returns without scanning.\n", root))
	m.viewport.GotoTop()
	m.resize()
}

func (m *model) confirmInitialization(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return tea.Quit
	case "esc", "f7", "f8", "ctrl+r":
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
		switch kind {
		case "import":
			m.importAction = true
		case "rescan":
			m.rescanAction = true
		default:
			m.initializeAction = true
		}
		return m.start(true)
	}
	return nil
}
