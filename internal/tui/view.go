package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/present"
)

// role is a colour's meaning. Each is one of the terminal's own ANSI
// colours, so the terminal theme sets its shade and its contrast on that
// theme's background; nothing depends on guessing the background.
type role int

const (
	accent role = iota
	success
	warning
	danger
	muted
	// netRole marks an action that uses the network.
	netRole
)

// paint styles text in a role. Without colour, text is plain; meaning never
// depends on colour alone.
func (m *model) paint(text string, r role, bold bool) string {
	if !m.color {
		return text
	}
	style := lipgloss.NewStyle().Bold(bold)
	switch r {
	case accent:
		style = style.Foreground(lipgloss.Blue)
	case success:
		style = style.Foreground(lipgloss.Green)
	case warning:
		style = style.Foreground(lipgloss.Yellow)
	case danger:
		style = style.Foreground(lipgloss.Red)
	case muted:
		style = style.Faint(true)
	case netRole:
		style = style.Foreground(lipgloss.Cyan)
	}
	return style.Render(text)
}

// navigation is plain mode's location line: the section and its steps, the
// current one bracketed, then where Tab and the palette lead.
func (m *model) navigation() string {
	s := sectionOf(m.mode)
	return clipWords(s.name+": "+stepList(s, m.mode)+"   Tab next step, Ctrl+O actions", max(1, m.width-2))
}

// clipWords shortens one row of text to width cells at a word boundary,
// ending with "...", so no word is cut.
func clipWords(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	words := strings.Fields(text)
	row := ""
	for _, w := range words {
		next := w
		if row != "" {
			next = row + " " + w
		}
		if lipgloss.Width(next)+3 > width {
			break
		}
		row = next
	}
	if row == "" {
		return clip(text, width)
	}
	return strings.TrimRight(row, ";,:|") + "..."
}

// fields is plain mode's form: one field per row with a fixed label column.
func (m *model) fields() string {
	fields := m.formFields()
	m.fieldAt, m.fieldX = [3]int{-1, -1, -1}, [3]int{}
	var rows []string
	for i, f := range fields {
		prefix := fmt.Sprintf("%s %-11s", fieldMarker(f.focused), f.label)
		m.fieldAt[f.slot], m.fieldX[f.slot] = i, lipgloss.Width(prefix)
		rows = append(rows, prefix+f.in.View())
	}
	return strings.Join(rows, "\n")
}

func (m *model) contextLine() string {
	if m.mode == "collections" {
		budget := "1 GiB"
		if m.collectionLargeBudget {
			budget = "5 GiB"
		}
		return "Offline status | F8 acquisition preview | F3 total transfer cap: " + budget
	}
	if m.mode == "revisions" {
		return "Offline declaration comparison | No holdings removed | No acquisition"
	}
	if m.mode == "read" {
		if m.reader.asset == "" {
			return "Local source access | Select an asset in Holdings, then r"
		}
		return fmt.Sprintf("Offline | Bookmark only on m | Asset %s", present.Exact(m.reader.asset))
	}
	if m.mode == "state" {
		return "Enter status | F10 jobs | F7 init | F8 import | F9 cleanup | Ctrl+P purge | Ctrl+R rescan"
	}
	if m.mode == "search" {
		if m.source == "gutenberg" {
			return "gutenberg; F2 provider | F3 order: " + m.searchOrder() + " | Local catalog; the Gutenberg website is not queried"
		}
		if m.source == "archive" {
			return "archive; F2 provider | F3 order: " + m.searchOrder() + " | Explicit network search"
		}
		if m.source == "openalex" {
			return "openalex; F2 changes provider | Explicit network search, keyless daily budget"
		}
		return m.source + "; F2 changes provider | Explicit network search"
	}
	if m.mode == "evaluate" {
		return "Source declarations | Enter evaluates | d reviews a download; a catalog row requests its record; an Open Library work lists editions"
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
		brand += "   Knowledge is power."
	}
	head := brand + "\n" + m.navigation() + "\n\n" + m.paint(clipWords(m.contextLine(), max(1, m.width-2)), muted, false) + "\n"
	m.fieldRow, m.fieldCol = strings.Count(head, "\n"), 0
	return head + m.fields()
}

func (m *model) footer() string {
	if m.collectionReview != nil {
		return lipgloss.Wrap(m.paint("Enter acquire  Esc preview  PgUp/PgDown review  Ctrl+C quit", warning, false), max(1, m.width-2), " ")
	}
	if m.reviewingDownload() {
		return lipgloss.Wrap(m.paint("Enter acquire  Esc back  PgUp/PgDown review  Ctrl+C quit", warning, false), max(1, m.width-2), " ")
	}
	if m.confirmRoot != "" {
		verb := map[string]string{"init": "initialize", "import": "import", "rescan": "rescan", "cleanup": "move to holding", "restore": "restore", "purge": "purge"}[m.confirmKind]
		return lipgloss.Wrap(m.paint("Enter "+verb+"  Esc back  PgUp/PgDown review  Ctrl+C quit", warning, false), max(1, m.width-2), " ")
	}
	if m.logOpen {
		return m.paint(clipWords("Up/Down scroll  Esc or L close  Ctrl+C quit", max(1, m.width-2)), muted, false)
	}
	if m.overlay != nil {
		return m.paint(clipWords("Up/Down choose  Enter run  Esc close  Ctrl+C quit", max(1, m.width-2)), muted, false)
	}
	if m.filter != nil && m.filter.editing {
		return m.paint(clipWords("Type to filter  Enter keep  Esc restore  Up/Down select  Ctrl+C quit", max(1, m.width-2)), muted, false)
	}
	if m.busy {
		return m.paint("Esc cancel  Ctrl+C quit", muted, false)
	}
	// The registry's top actions for this focus; whole keys only, and the
	// last (quit) kept when space is short.
	keys := m.footerKeys()
	parts := make([]string, len(keys))
	for i, c := range keys {
		parts[i] = footerKey(c) + " " + c.label
	}
	for len(parts) > 1 && lipgloss.Width(strings.Join(parts, "  ")) > max(1, m.width-2) {
		parts = append(parts[:len(parts)-2], parts[len(parts)-1])
	}
	return m.paint(clip(strings.Join(parts, "  "), max(1, m.width-2)), muted, false)
}

func (m *model) View() tea.View {
	if m.width < 40 || m.height < 20 {
		lines := strings.Split(lipgloss.Wrap("Nemalo\nTerminal too small. Resize to at least 40 columns and 20 rows.\nCtrl+C quits.", max(1, m.width), " "), "\n")
		v := tea.NewView(strings.Join(lines[:min(len(lines), max(1, m.height))], "\n"))
		v.AltScreen = true
		return v
	}
	if m.pager != nil {
		v := tea.NewView(m.pagerView())
		v.Cursor = m.findCursor()
		v.AltScreen = true
		return v
	}
	if m.readAux != nil {
		v := tea.NewView(m.readAuxView())
		v.AltScreen = true
		return v
	}
	if !m.plain {
		content, cursor := m.panelView()
		v := tea.NewView(content)
		v.AltScreen = true
		v.Cursor = cursor
		// Mouse selection is a convenience over the complete keyboard path;
		// plain mode leaves the terminal's own mouse alone (decision 0103).
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}
	state := m.statusLine()
	position := m.position()
	content := m.browseView()
	if m.viewport.GetContent() == "" && !m.busy {
		lines := strings.Split(lipgloss.Wrap(m.emptyHelp(), m.width-2, " "), "\n")
		content = lipgloss.NewStyle().Height(m.viewport.Height()).Render(strings.Join(lines[:min(len(lines), m.viewport.Height())], "\n"))
	}
	if m.busy && m.viewport.GetContent() == "" {
		lines := m.busyLines(m.width - 2)
		content = lipgloss.NewStyle().Height(m.viewport.Height()).Render(strings.Join(lines[:min(m.viewport.Height(), len(lines))], "\n"))
	}
	if m.logOpen {
		content = lipgloss.NewStyle().Height(m.viewport.Height()).Render(strings.Join(m.logLines(m.width-2, m.viewport.Height()), "\n"))
	}
	overlayRow := -1
	if m.overlay != nil {
		lines, row := m.overlayLines(m.width-2, m.viewport.Height())
		overlayRow = row
		content = lipgloss.NewStyle().Height(m.viewport.Height()).Render(strings.Join(lines, "\n"))
	}
	head := m.top() + "\n" + state + "\n\n" + m.paint(position, muted, false) + "\n" + m.paint(strings.Repeat("-", m.width-2), muted, false) + "\n"
	v := tea.NewView(head + content + "\n" + m.footer())
	v.AltScreen = true
	v.Cursor = m.screenCursor(strings.Count(head, "\n"), overlayRow)
	return v
}

// screenCursor places plain mode's real terminal cursor at the chosen list
// item or at the focused field's edit position (decision 0094). contentRow is
// the first row of the content area.
func (m *model) screenCursor(contentRow, overlayRow int) *tea.Cursor {
	if m.overlay != nil {
		if overlayRow < 0 {
			return nil
		}
		return tea.NewCursor(0, contentRow+overlayRow)
	}
	if m.filter != nil && m.filter.editing && m.filterLine >= 0 {
		return inputCursor(m.filter.input, lipgloss.Width("> Filter "), contentRow+m.filterLine, m.width)
	}
	if m.resultsFocused || m.reviewing() {
		return nil
	}
	for i, in := range []textinput.Model{m.input, m.secondary, m.intake} {
		if in.Focused() && m.fieldAt[i] >= 0 {
			return inputCursor(in, m.fieldX[i], m.fieldRow+m.fieldAt[i], m.width)
		}
	}
	return nil
}

func (m *model) resize() {
	if !m.plain {
		m.resizePanel()
		return
	}
	fields := m.formFields()
	widths := make([]int, len(fields))
	for i := range widths {
		widths[i] = max(1, m.width-16)
	}
	m.setFieldWidths(fields, widths)
	m.viewport.SetWidth(max(1, m.width-2))
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
	m.viewport.SetContent(hardWrap(text, max(1, m.viewport.Width())))
}

// hardWrap keeps every displayed line inside the viewport, including paths that
// contain no spaces. The stored report text is unchanged.
func hardWrap(text string, width int) string {
	if width < 1 {
		width = 1
	}
	var b strings.Builder
	lines := strings.Split(lipgloss.Wrap(text, width, ""), "\n")
	for i, line := range lines {
		for lipgloss.Width(line) > width {
			cut := len(line)
			for cut > 0 && lipgloss.Width(line[:cut]) > width {
				cut--
				for cut > 0 && !utf8.RuneStart(line[cut]) {
					cut--
				}
			}
			if cut < 1 {
				_, size := utf8.DecodeRuneInString(line)
				cut = max(1, size)
			}
			b.WriteString(line[:cut])
			b.WriteByte('\n')
			line = line[cut:]
		}
		b.WriteString(line)
		if i != len(lines)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m *model) statusText() string {
	lines := strings.Split(lipgloss.Wrap(m.status, max(1, m.width-2), " "), "\n")
	if len(lines) > 2 {
		return lines[0] + "\n" + "Full error is in results."
	}
	return strings.Join(lines, "\n")
}

func (m *model) position() string {
	if m.collectionReview != nil {
		return fmt.Sprintf("Review collection acquisition | Scroll %.0f%%", m.viewport.ScrollPercent()*100)
	}
	if m.reviewingDownload() {
		return fmt.Sprintf("Review acquisition | Scroll %.0f%%", m.viewport.ScrollPercent()*100)
	}
	if m.confirmRoot != "" {
		label := map[string]string{"init": "initialization", "import": "import", "rescan": "rescan", "cleanup": "cleanup", "restore": "restore", "purge": "purge"}[m.confirmKind]
		return fmt.Sprintf("Review %s | Scroll %.0f%%", label, m.viewport.ScrollPercent()*100)
	}
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
	case "home":
		return "Start here.\n\nEnter an existing library folder and press Enter for an offline overview: its status, holdings, scanner, saved bookmarks and recorded jobs. Nothing is written.\nScripts and agents: nemalo COMMAND --json, or nemalo mcp serve. See nemalo help."
	case "search":
		return "Find something worth keeping.\n\nEnter a title, author, or topic. F2 selects Open Library, Internet Archive, or a local Project Gutenberg catalog.\nEnter searches that provider. Gutenberg reads the catalog file and does not contact gutenberg.org.\nDiscovery does not download files."
	case "scan":
		return "Scan one file with the installed antivirus.\n\nRecommended for a file you did not produce. Windows uses Microsoft Defender; Linux and macOS use ClamAV.\nThe scanner's own cloud and sample-submission settings apply. Nothing is remediated.\nA missing, failed, or incomplete scan is not clean, and no detection is not proof of safety."
	case "holdings":
		return "Browse your collection.\n\nChoose a saved catalog and optionally filter by title or language.\nEPUB filenames are selected by default; F4 changes the format."
	case "evaluate":
		return "Look before acquiring.\n\nEnter archive:ITEM, or gutenberg:NUMBER. Select a search result and press e.\nEnter retrieves metadata only. A Gutenberg catalog row lists no files.\nWith an intake folder, d on that row requests one bibliographic record and then lists EPUB offers.\nF6 browses source files when the evaluation lists any.\nEnter a new intake folder with Ctrl+N; select a file and press d to review acquisition.\nDownloads remain untrusted; existing intake is never overwritten."
	case "state":
		return "Establish a library identity.\n\nEnter an existing folder, then Enter to read its control status.\nF7 reviews initialization before writing control metadata.\nCtrl+N sets one source. F8 reviews import and does not scan or delete it.\nCtrl+R reviews a scan of review holdings that lack scan evidence.\nF9 previews moving the source folder's held files to the library's recoverable holding area; with a cleanup ID as the source, it previews restoring that cleanup.\nCtrl+P previews purging that cleanup's held copies the library holds; Enter removes exactly those, and the library keeps its own copy."
	case "read":
		return "Read from exact source bytes.\n\nSelect an asset in Holdings and press r. Enter its explicit root, then Enter lists units.\nF6 browses units; Enter retrieves text. n continues, r repeats, u lists units.\nc lists the book's declared contents. Enter reads one entry and stops at its declared end.\ng lists the EPUB's declared page labels, such as iv or 42; Enter reads from one. They are the book's own markers, not screen pages.\nF2 shows recorded citation metadata. F4 shows source-bound evidence for the last successful excerpt; F3 explicitly replays it in that report.\np opens the selection as pages sized to the terminal; s opens it one word at a time.\nm saves a bookmark in an initialized library. ' restores that bookmark.\nDelivery does not record reading progress, and closing does not save a bookmark.\nNotes and image resources are available through content CLI references."
	case "collections":
		return "Resolve exact selections against your library.\n\nEnter a collection JSON file and an existing initialized library. Ctrl+N moves through collection, library and intake.\nEnter reads status offline. F6 browses declared items; F5 shows the full report.\nWith an absolute outside-library intake, F8 previews acquisition offline. A second Enter confirms the bound review before requests or writes.\nF3 chooses a 1 GiB or 5 GiB total transfer cap. Downloads remain untrusted intake; ingest is separate.\nAlt+B compares two explicit revisions without changing holdings or files."
	case "jobs":
		return "Observe recorded jobs for one library.\n\nEnter an existing initialized library folder, then Enter (or F10) reads its recorded jobs through the passive snapshot, as F10 in Manage does.\nNothing is reconciled, polled, started or cancelled. A recorded running job can be abandoned; ownership is not observed.\nF6 browses jobs; F5 shows observation scope, coverage and limits."
	case "revisions":
		return "Compare two collection declarations offline.\n\nEnter the before and after JSON files, using Ctrl+N to switch fields. Enter compares exact selections, notes, name and retained order.\nF6 browses changes; F5 shows both file hashes and all limitations.\nA removed selection never removes holdings or notes. This comparison grants no acquisition authority."
	default:
		return "Enter the fields above, then press Enter.\n\nResults stay with their operation when you switch modes.\nSources are preserved. Escape cancels active work."
	}
}
