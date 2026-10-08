package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	accent  = "#A5B4FC"
	muted   = "#94A3B8"
	warning = "#FBBF24"
	danger  = "#FCA5A5"
)

func (m *model) paint(text, color string, bold bool) string {
	if !m.color {
		return text
	}
	if !m.dark {
		if light, ok := map[string]string{accent: "#4338CA", muted: "#475569", warning: "#854D0E", danger: "#B91C1C"}[color]; ok {
			color = light
		}
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(bold).Render(text)
}

func (m *model) navigation() string {
	items := make([]string, len(modes))
	for i, name := range modes {
		label := fmt.Sprintf("%d %s", i+1, strings.ToUpper(name[:1])+name[1:])
		if name == m.mode {
			label = m.paint("["+label+"]", accent, true)
		}
		items[i] = label
	}
	return lipgloss.Wrap(strings.Join(items, "  "), max(1, m.width-2), " ")
}

func (m *model) fields() string {
	label, hint, second := "Query", "Search books (network on Enter)", ""
	switch m.mode {
	case "inspect":
		label, hint = "Folder", "Explicit folder for read-only inventory"
	case "check":
		label, hint = "File", "Explicit file for local health checks"
	case "scan":
		label, hint = "File", "File for health + antivirus; scanner cloud/sample policy applies"
	case "snapshot":
		label, hint, second = "Folder", "Explicit source folder", "New output"
	case "holdings":
		label, hint, second = "Catalog", "Existing catalog file", "Filter"
	case "audit":
		label, hint, second = "Catalog", "Existing catalog file", "Root"
	case "evaluate":
		label, hint = "Item ID", "archive:ITEM (network metadata only; no download)"
	}
	m.input.Placeholder = hint
	marker := " "
	if !m.resultsFocused && !m.secondaryFocused {
		marker = ">"
	}
	s := fmt.Sprintf("%s %-10s%s", marker, label, m.input.View())
	if second != "" {
		marker = " "
		if !m.resultsFocused && m.secondaryFocused {
			marker = ">"
		}
		s += fmt.Sprintf("\n%s %-10s%s", marker, second, m.secondary.View())
	}
	return s
}

func (m *model) contextLine() string {
	if m.mode == "search" {
		return m.source + "; F2 changes provider | Explicit network search"
	}
	if m.mode == "evaluate" {
		return "Source declarations | No file download or safety verdict"
	}
	if m.mode == "scan" {
		return "Explicit antivirus | Host cloud/sample policy applies; no remediation"
	}
	if m.mode == "holdings" {
		return "Local | F4: filename format " + m.format
	}
	if m.mode == "snapshot" || m.mode == "audit" || m.mode == "inspect" {
		budget := "1 GiB"
		if m.largeBudget {
			budget = "5 GiB"
		}
		line := "Local files | Sources preserved | F3: read budget " + budget
		if m.mode == "snapshot" {
			line += fmt.Sprintf(" | Ctrl+A: health metadata %t", m.assess)
		}
		return line
	}
	return "Local and offline | Sources preserved"
}

func (m *model) top() string {
	brand := m.paint("Nemalo", accent, true)
	if m.width >= 70 {
		brand += "   Find knowledge. Care for it. Realize its potential."
	}
	return brand + "\n" + m.navigation() + "\n\n" + lipgloss.Wrap(m.paint(m.contextLine(), muted, false), max(1, m.width-2), " ") + "\n" + m.fields()
}

func (m *model) footer() string {
	help := "Enter run  F6 browse  Tab/Shift+Tab modes  Ctrl+C quit"
	if m.resultsFocused {
		help = "Up/Down select  Enter details  F6 edit  Esc back  Ctrl+C quit"
		if m.mode == "search" && len(m.entries) > 0 && strings.HasPrefix(m.entries[m.selected].sourceID, "archive:") {
			help += "  e evaluate"
		}
	}
	if m.busy {
		help = "Working...  Esc cancel  Ctrl+C quit"
		return lipgloss.Wrap(m.paint(help, muted, false), max(1, m.width-2), " ")
	}
	if m.form() {
		help += "  Ctrl+N field"
	}
	help += "\nF5 full report  PgUp/PgDown scroll  Alt+1..8 jump"
	if m.mode == "search" || m.mode == "holdings" {
		help += "  Ctrl+Left/Right results page"
	}
	return lipgloss.Wrap(m.paint(help, muted, false), max(1, m.width-2), " ")
}

func (m *model) View() tea.View {
	if m.width < 40 || m.height < 20 {
		lines := strings.Split(lipgloss.Wrap("Nemalo\nTerminal too small. Resize to at least 40 columns and 20 rows.\nCtrl+C quits.", max(1, m.width), " "), "\n")
		v := tea.NewView(strings.Join(lines[:min(len(lines), max(1, m.height))], "\n"))
		v.AltScreen = true
		return v
	}
	state := m.statusText()
	color := accent
	if m.busy {
		color = warning
	} else if strings.Contains(state, "Incomplete") || strings.Contains(state, "error") {
		color = danger
	}
	state = lipgloss.Wrap(m.paint(state, color, false), m.width-2, " ")
	position := m.position()
	content := m.browseView()
	if m.viewport.GetContent() == "" && !m.busy {
		lines := strings.Split(lipgloss.Wrap(m.emptyHelp(), m.width-2, " "), "\n")
		content = lipgloss.NewStyle().Height(m.viewport.Height()).Render(strings.Join(lines[:min(len(lines), m.viewport.Height())], "\n"))
	}
	text := m.top() + "\n" + state + "\n\n" + m.paint(position, muted, false) + "\n" + m.paint(strings.Repeat("-", m.width-2), muted, false) + "\n" + content + "\n" + m.footer()
	v := tea.NewView(text)
	v.AltScreen = true
	return v
}

func (m *model) resize() {
	m.input.SetWidth(max(1, m.width-16))
	m.secondary.SetWidth(max(1, m.width-16))
	m.secondary.Placeholder = map[string]string{"snapshot": "New catalog path outside the source folder", "holdings": "Optional title, language, filename, or hash", "audit": "Explicit source directory (required)"}[m.mode]
	// Layout height follows the actual wrapped navigation, context, and help.
	reserved := lipgloss.Height(m.top()) + lipgloss.Height(m.footer()) + lipgloss.Height(m.statusText()) + lipgloss.Height(m.position()) + 2
	m.viewport.SetHeight(max(1, m.height-reserved))
	m.setReport(m.report)
	m.preview.SetHeight(m.viewport.Height())
	width := m.viewport.Width()
	if !m.details && width >= 98 {
		width -= width/2 + 3
	}
	m.preview.SetWidth(width)
	m.refreshPreview()
}

func (m *model) setReport(text string) {
	m.report = text
	m.viewport.SetContent(lipgloss.Wrap(text, max(1, m.viewport.Width()), ""))
}

func (m *model) statusText() string {
	lines := strings.Split(lipgloss.Wrap(m.status, max(1, m.width-2), " "), "\n")
	if len(lines) > 2 {
		return lines[0] + "\n" + "Full error is in results."
	}
	return strings.Join(lines, "\n")
}

func (m *model) position() string {
	position := fmt.Sprintf("Results | Scroll %.0f%%", m.viewport.ScrollPercent()*100)
	if m.report == "" {
		position = "Results | Not loaded"
		if m.busy {
			position = "Results | Working"
		}
	}
	if len(m.entries) > 0 && !m.evidence {
		position = fmt.Sprintf("Results | Selected %d of %d on this page", m.selected+1, len(m.entries))
		if m.details {
			position = fmt.Sprintf("Item details | Scroll %.0f%%", m.preview.ScrollPercent()*100)
		}
	} else if m.evidence {
		position = fmt.Sprintf("Full evidence report | Scroll %.0f%%", m.viewport.ScrollPercent()*100)
	}
	if (m.mode == "search" || m.mode == "holdings") && m.total > 0 {
		position += fmt.Sprintf(" | %d matches, offset %d", m.total, m.offset)
	}
	if m.resultsFocused {
		position = "> " + position
	} else {
		position = "  " + position
	}
	return lipgloss.Wrap(position, max(1, m.width-2), " ")
}

func (m *model) emptyHelp() string {
	switch m.mode {
	case "search":
		return "Find something worth keeping.\n\nEnter a title, author, or topic. F2 selects Open Library or Internet Archive.\nEnter searches that provider. Discovery does not download files."
	case "holdings":
		return "Browse your collection.\n\nChoose a saved catalog and optionally filter by title or language.\nEPUB filenames are selected by default; F4 changes the format."
	case "evaluate":
		return "Look before acquiring.\n\nEnter archive:ITEM, or select an Archive search result and press e.\nEnter retrieves declared files, restrictions, and rights metadata only."
	default:
		return "Enter the fields above, then press Enter.\n\nResults stay with their operation when you switch modes.\nSources are preserved. Escape cancels active work."
	}
}
