package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/present"
)

type entry struct{ title, summary, detail, sourceID string }

// Browsing is a presentation of shared service results, not another catalog.
func (m *model) populate(data any) {
	m.entries, m.selected, m.details, m.evidence = nil, 0, false, false
	switch p := data.(type) {
	case discovery.Evaluation:
		for _, f := range p.Files {
			size := "size not declared"
			if f.Bytes != nil {
				size = byteSize(*f.Bytes)
			}
			m.entries = append(m.entries, entry{title: fmt.Sprintf("%q", f.Name), summary: fmt.Sprintf("%s | %q", size, f.Access), detail: fmt.Sprintf("File: %q\nSource format: %q\nSize: %s\nAccess: %q\nRights: %q\nDRM: %q\n\nFindings: %q\n\nItem rights: %q\nLicense declarations: %q\n\nUse d to review an explicit intake download. Availability is resolved again before transfer; restricted, uncertain, unsupported, or unbounded files are refused.\n", f.Name, f.Format, size, f.Access, f.RightsStatus, f.DRMStatus, f.Findings, p.Rights, p.LicenseURLs), sourceID: f.Name})
		}
	case discovery.Page:
		for _, r := range p.Results {
			m.entries = append(m.entries, entry{
				title:   fmt.Sprintf("%q", r.Title),
				summary: fmt.Sprintf("%q | %q", r.Authors, r.Languages),
				detail:  fmt.Sprintf("%q\n\nAuthors: %q\nLanguages: %q\n\nDiscovery metadata only.\nDownload availability not resolved.\n\nID: %q\nSource page: %q\nDeclared access: %q\n", r.Title, r.Authors, r.Languages, r.ID, r.LandingPage, r.Access), sourceID: r.ID,
			})
		}
	case library.Page:
		for _, a := range p.Assets {
			title, languages := a.ID, []string{}
			if len(a.Locations) > 0 {
				title = a.Locations[0].Path
			}
			if a.Health != nil && a.Health.Checks.EPUB != nil {
				e := a.Health.Checks.EPUB
				languages = e.Languages
				if len(e.Titles) > 0 {
					title = e.Titles[0]
				}
			}
			detail := fmt.Sprintf("%q\n\nLanguages: %q\nSize: %s (%d bytes)\n\n", title, languages, byteSize(a.Bytes), a.Bytes)
			if a.Health != nil {
				detail += fmt.Sprintf("Recorded health: %q\nAntivirus: %q\n", strings.ReplaceAll(a.Health.Status, "_", " "), strings.ReplaceAll(a.Health.Antivirus.Status, "_", " "))
			} else {
				detail += "No recorded assessment.\nAntivirus: not recorded\n"
			}
			detail += "Historical catalog evidence.\nAudit to check current bytes.\n\nLocations (filename candidates):\n"
			for _, l := range a.Locations {
				detail += fmt.Sprintf("  %q [%q]\n", l.Path, l.CandidateKind)
			}
			detail += fmt.Sprintf("\nID: %q\n", a.ID)
			if a.Health != nil {
				detail += "\nRecorded assessment (historical):\n" + present.Health(*a.Health)
			}
			m.entries = append(m.entries, entry{
				title:    fmt.Sprintf("%q", title),
				summary:  fmt.Sprintf("%s | %q | %d location(s)", byteSize(a.Bytes), languages, len(a.Locations)),
				detail:   detail,
				sourceID: a.ID,
			})
		}
	case content.Result:
		if p.Status == "units_available" {
			for _, u := range p.Units {
				m.entries = append(m.entries, entry{title: fmt.Sprintf("Unit %d: %q", u.Index, u.Member), summary: fmt.Sprintf("%d text bytes | %d gaps | %q", u.TextBytes, u.KnownGaps, u.Language), detail: fmt.Sprintf("Source unit %d\n\nMember: %q\nLanguage: %q\nDirection: %q\nLinear: %q\nParts: %d; known text gaps: %d\n\nEnter retrieves a bounded structured-text range.\nNo reading progress is recorded.", u.Index, u.Member, u.Language, u.Direction, u.Linear, u.Parts, u.KnownGaps)})
			}
		}
	}
	m.selectEntry(0)
}

func byteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1<<20 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
}

func (m *model) selectEntry(index int) {
	if len(m.entries) == 0 {
		m.preview.SetContent("")
		return
	}
	m.selected = max(0, min(index, len(m.entries)-1))
	m.refreshPreview()
	m.preview.GotoTop()
}

func (m *model) refreshPreview() {
	if len(m.entries) > 0 {
		// Pre-wrap at grapheme boundaries. The viewport's cell slicing can split
		// wide characters, causing its renderer to wrap an already sliced line.
		m.preview.SetContent(lipgloss.Wrap(m.entries[m.selected].detail, max(1, m.preview.Width()), ""))
	}
}

func (m *model) focusForm() tea.Cmd {
	m.resultsFocused = false
	m.resize()
	if m.secondaryFocused {
		return m.secondary.Focus()
	}
	return m.input.Focus()
}

func (m *model) browserKey(key string) (bool, tea.Cmd) {
	if m.busy {
		return false, nil
	}
	switch key {
	case "f5":
		if m.report == "" {
			return true, nil
		}
		m.evidence = !m.evidence
		m.details = false
		m.resize()
		return true, nil
	case "esc":
		if m.details || m.evidence {
			m.details, m.evidence = false, false
			m.resize()
			return true, nil
		}
	case "f6":
		if m.resultsFocused {
			return true, m.focusForm()
		}
		m.resultsFocused = true
		m.input.Blur()
		m.secondary.Blur()
		m.resize()
		return true, nil
	}
	if !m.resultsFocused {
		return false, nil
	}
	switch key {
	case "d":
		if m.mode == "evaluate" && len(m.entries) > 0 {
			m.requestDownload()
			return true, nil
		}
	case "esc":
		return true, m.focusForm()
	case "up", "down", "home", "end":
		if !m.details && !m.evidence {
			index := m.selected
			switch key {
			case "up":
				index--
			case "down":
				index++
			case "home":
				index = 0
			case "end":
				index = len(m.entries) - 1
			}
			m.selectEntry(index)
			return true, nil
		}
	case "enter":
		if m.mode == "read" && len(m.entries) > 0 {
			return true, m.startRead(false, m.selected, "", false)
		}
		if len(m.entries) > 0 {
			m.details, m.evidence = !m.details, false
			m.resize()
		}
		return true, nil
	case "e":
		if len(m.entries) > 0 && m.mode == "search" && strings.HasPrefix(m.entries[m.selected].sourceID, "archive:") {
			id := m.entries[m.selected].sourceID
			cmd := m.switchMode("evaluate")
			m.input.SetValue(id)
			m.status = "Selected item. Enter requests Archive metadata; no download."
			m.resize()
			return true, cmd
		}
	case "r":
		if m.mode == "holdings" && len(m.entries) > 0 {
			asset, catalog := m.entries[m.selected].sourceID, m.submittedInput
			cmd := m.switchMode("read")
			m.reader = readerState{asset: asset}
			m.input.SetValue(catalog)
			m.secondary.SetValue("")
			m.setReport("")
			m.populate(nil)
			m.status = "Selected asset. Enter an explicit root, then Enter lists source units."
			m.resize()
			return true, cmd
		}
		if m.mode == "read" {
			return true, m.startRead(false, 0, "", true)
		}
	case "n":
		if m.mode == "read" && m.reader.next != "" {
			return true, m.startRead(false, 0, m.reader.next, false)
		}
	case "u":
		if m.mode == "read" {
			return true, m.startRead(true, 0, "", false)
		}
	}
	return false, nil
}

func clip(text string, width int) string {
	// Input is already quoted; Lip Gloss truncates by terminal cells, not bytes.
	if lipgloss.Width(text) > width && width > 3 {
		return lipgloss.NewStyle().MaxWidth(width-3).Render(text) + "..."
	}
	return lipgloss.NewStyle().MaxWidth(max(1, width)).Render(text)
}

func (m *model) browseView() string {
	height, width := m.viewport.Height(), m.viewport.Width()
	if m.evidence || len(m.entries) == 0 {
		return m.viewport.View()
	}
	if m.details {
		return m.preview.View()
	}
	listWidth := width
	if width >= 98 {
		listWidth = width / 2
	}
	count := max(1, height/3)
	start := (m.selected / count) * count
	var rows []string
	for i := start; i < min(len(m.entries), start+count); i++ {
		e := m.entries[i]
		marker := " "
		if i == m.selected {
			marker = ">"
		}
		title := fmt.Sprintf("%s %2d  %s", marker, m.offset+i+1, e.title)
		if i == m.selected {
			title = m.paint(clip(title, listWidth-2), accent, true)
		} else {
			title = clip(title, listWidth-2)
		}
		rows = append(rows, title, m.paint(clip("      "+e.summary, listWidth-2), muted, false), "")
	}
	list := lipgloss.NewStyle().Width(listWidth).Height(height).MaxHeight(height).Render(strings.Join(rows, "\n"))
	if listWidth == width {
		return list
	}
	divider := strings.TrimSuffix(strings.Repeat(" | \n", height), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, list, m.paint(divider, muted, false), m.preview.View())
}
