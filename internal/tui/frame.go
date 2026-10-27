package tui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/present"
	"github.com/charmbracelet/x/ansi"
)

// The panel frame (decision 0101): one header row, a sections sidebar at 100
// columns and wider or section names on the header row below that, titled
// panels with the form inside, then the status line and a one-line footer.
// Plain mode keeps the linear layout in view.go.

const sidebarWidth = 22

// frame is the panel geometry for the current size and state.
type frame struct {
	header   []string
	side     int // sidebar width, 0 when narrow
	top      int // first panel row
	height   int // panel rows, borders included
	mainW    int // main panel width, borders included
	detailW  int // detail panel width, 0 without a split
	formRows int
	contentH int // rows below the form inside the main panel
	inner    int // main panel text width
}

type formField struct {
	label   string
	in      *textinput.Model
	focused bool
	slot    int
}

// formFields is the current step's fields, in order. Labels, hints and focus
// are shared by both layouts.
func (m *model) formFields() []formField {
	label, hint, second := "Query", "Search books (network on Enter)", ""
	switch m.mode {
	case "inspect":
		label, hint = "Folder", "Explicit folder for read-only inventory"
	case "check":
		label, hint = "File", "Explicit file for local health checks"
	case "scan":
		label, hint = "File", "Recommended for a file you did not produce; scanner cloud/sample policy applies; not proof of safety"
	case "snapshot":
		label, hint, second = "Folder", "Explicit source folder", "New output"
	case "holdings":
		label, hint, second = "Catalog", "Existing catalog file", "Filter"
	case "audit":
		label, hint, second = "Catalog", "Catalog file, or a library folder to check", "Root"
	case "evaluate":
		label, hint, second = "Item ID", "archive:ITEM, gutenberg:NUMBER, or openlibrary:WORK/EDITION", "Intake"
	case "state":
		label, hint, second = "Folder", "Existing library folder; Enter reads status", "Source"
	case "jobs":
		label, hint = "Library", "Existing initialized library folder; Enter reads recorded jobs"
	case "home":
		label, hint = "Library", "Existing library folder; Enter reads its overview offline"
	case "read":
		label, hint, second = "Catalog", "Catalog selected from Holdings", "Root"
	case "collections":
		label, hint, second = "Collection", "Existing exact-selection JSON file", "Library"
	case "revisions":
		label, hint, second = "Before", "Earlier collection JSON file", "After"
	}
	if m.mode == "search" && m.source == "gutenberg" {
		label, hint, second = "Query", "Title, author, subject, or text number", "Catalog"
	}
	m.inputHint = hint
	fields := []formField{{label, &m.input, !m.resultsFocused && !m.secondaryFocused && !m.intakeFocused, 0}}
	if second != "" {
		fields = append(fields, formField{second, &m.secondary, !m.resultsFocused && m.secondaryFocused, 1})
	}
	if m.mode == "collections" {
		fields = append(fields, formField{"Intake", &m.intake, !m.resultsFocused && m.intakeFocused, 2})
	}
	return fields
}

// setFieldWidths sizes each input and clips its hint to whole words.
func (m *model) setFieldWidths(fields []formField, widths []int) {
	for i, f := range fields {
		f.in.SetWidth(max(1, widths[i]))
		f.in.SetCursor(f.in.Position())
	}
	m.applyHints(func(slot int) int {
		for i, f := range fields {
			if f.slot == slot {
				return widths[i]
			}
		}
		return 1
	})
}

// applyHints sets placeholders clipped to each field's width.
func (m *model) applyHints(width func(slot int) int) {
	m.input.Placeholder = clipWords(m.inputHint, max(1, width(0)))
	m.intake.Placeholder = clipWords("Absolute outside-library intake; F8 previews without writing", max(1, width(2)))
	hint := map[string]string{"snapshot": "New catalog path outside the source folder", "holdings": "Optional title, language, filename, or hash", "audit": "Source directory for a catalog; leave empty to check a library", "read": "Explicit source directory (required)", "evaluate": "New untrusted intake directory, for d acquire", "state": "EPUB, PDF, MP3, M4B, M4A, or completed intake packet", "collections": "Existing initialized library folder", "revisions": "Later collection JSON file"}[m.mode]
	if m.mode == "search" && m.source == "gutenberg" {
		hint = "Absolute path of pg_catalog.csv"
		if m.catalog != "" {
			hint = m.catalog
		}
	}
	m.secondary.Placeholder = clipWords(hint, max(1, width(1)))
}

// formLayout places the fields in width cells: on one row when every input
// gets at least 20 cells, else one per row with aligned labels. It records
// each field's row and value column for the real cursor.
func (m *model) formLayout(width int) []string {
	fields := m.formFields()
	labels := 0
	for _, f := range fields {
		labels += lipgloss.Width(f.label) + 3
	}
	m.fieldAt, m.fieldX = [3]int{-1, -1, -1}, [3]int{}
	gaps := 2 * (len(fields) - 1)
	each := (width - labels - gaps) / len(fields)
	widths := make([]int, len(fields))
	if len(fields) > 1 && each >= 20 {
		for i := range widths {
			widths[i] = each
		}
		m.setFieldWidths(fields, widths)
		row, x := "", 0
		for i, f := range fields {
			if i > 0 {
				row += "  "
				x += 2
			}
			prefix := fieldMarker(f.focused) + " " + f.label + " "
			m.fieldAt[f.slot], m.fieldX[f.slot] = 0, x+lipgloss.Width(prefix)
			row += prefix + lipgloss.NewStyle().Width(each).MaxWidth(each).Render(f.in.View())
			x += lipgloss.Width(prefix) + each
		}
		return []string{row}
	}
	column := 0
	for _, f := range fields {
		column = max(column, lipgloss.Width(f.label))
	}
	for i := range widths {
		widths[i] = max(1, width-column-3)
	}
	m.setFieldWidths(fields, widths)
	lines := make([]string, len(fields))
	for i, f := range fields {
		prefix := fmt.Sprintf("%s %-*s ", fieldMarker(f.focused), column, f.label)
		m.fieldAt[f.slot], m.fieldX[f.slot] = i, lipgloss.Width(prefix)
		lines[i] = prefix + f.in.View()
	}
	return lines
}

func fieldMarker(focused bool) string {
	if focused {
		return ">"
	}
	return " "
}

// split reports whether results sit beside a detail panel.
func (m *model) split(width int) bool {
	return width >= 100 && m.overlay == nil && len(m.entries) > 0 && !m.details && !m.evidence && !m.reviewing()
}

func (m *model) reviewing() bool {
	return m.confirmRoot != "" || m.collectionReview != nil || m.reviewingDownload()
}

// layout computes the frame and sizes the inputs, report and preview to it.
func (m *model) layout() frame {
	f := frame{}
	if m.width >= 100 {
		f.side = sidebarWidth
		left := "Nemalo  " + sectionOf(m.mode).name
		if stepTitle(m.mode) != sectionOf(m.mode).name {
			left += " / " + stepTitle(m.mode)
		}
		f.header = []string{m.paint("Nemalo", accent, true) + strings.TrimPrefix(left, "Nemalo")}
		right := m.netWord()
		if gap := m.width - lipgloss.Width(left) - lipgloss.Width(right); gap >= 2 {
			f.header[0] += strings.Repeat(" ", gap) + m.netPaint(right)
		}
	} else {
		f.header = m.tabRows()
	}
	f.top = len(f.header)
	f.height = max(3, m.height-f.top-lipgloss.Height(m.statusLine())-lipgloss.Height(m.footer()))
	full := m.width - f.side
	f.mainW = full
	if m.split(m.width) {
		f.mainW = full * 11 / 20
		f.detailW = full - f.mainW
	}
	f.inner = max(1, f.mainW-4)
	if m.overlay == nil && !m.reviewing() {
		f.formRows = len(m.formLayout(f.inner))
		if m.caveat() != "" {
			f.formRows += lipgloss.Height(lipgloss.Wrap(m.caveat(), f.inner, " "))
		}
	}
	f.contentH = max(1, f.height-2-f.formRows)
	return f
}

// tabRows names every section on the header row, wrapping whole names.
func (m *model) tabRows() []string {
	current := sectionOf(m.mode).name
	rows := []string{m.paint("Nemalo", accent, true)}
	width := lipgloss.Width("Nemalo")
	for _, s := range sections {
		name := s.name
		shown := name
		if name == current {
			name = "[" + name + "]"
			shown = m.paint(name, accent, true)
		}
		if width+2+lipgloss.Width(name) > m.width {
			rows = append(rows, shown)
			width = lipgloss.Width(name)
			continue
		}
		rows[len(rows)-1] += "  " + shown
		width += 2 + lipgloss.Width(name)
	}
	return rows
}

func (m *model) netWord() string {
	switch {
	case m.mode == "scan":
		return "scanner policy applies"
	case m.usesNetwork():
		return "network on Enter"
	}
	return "offline"
}

func (m *model) netPaint(word string) string {
	if word == "network on Enter" {
		return m.paint(word, netRole, false)
	}
	return m.paint(word, muted, false)
}

// box draws a panel: a rounded border, or a heavy one when focused, with the
// title and state in the top border. Lines are clipped and padded to fit.
func (m *model) box(title, right string, lines []string, width, height int, focused bool, tone role) string {
	corners := []string{"╭", "╮", "╰", "╯", "─", "│"}
	if focused {
		corners = []string{"┏", "┓", "┗", "┛", "━", "┃"}
	}
	inner := max(1, width-4)
	paintBorder := func(s string) string {
		if focused {
			return m.paint(s, tone, true)
		}
		return m.paint(s, muted, false)
	}
	title = clip(title, max(1, width-6))
	room := width - 6 - lipgloss.Width(title)
	if right != "" && room > lipgloss.Width(right)+3 {
		right = " " + right + " "
	} else {
		right = ""
	}
	fill := max(0, width-6-lipgloss.Width(title)-lipgloss.Width(right))
	shownTitle := title
	if focused {
		shownTitle = m.paint(title, tone, true)
	}
	top := paintBorder(corners[0]+corners[4]) + " " + shownTitle + " " + paintBorder(strings.Repeat(corners[4], fill)) + m.paint(right, muted, false) + paintBorder(corners[4]+corners[1])
	if width < 8 {
		top = paintBorder(strings.Repeat(corners[4], max(0, width)))
	}
	out := []string{top}
	side := paintBorder(corners[5])
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], inner, "")
		}
		out = append(out, side+" "+line+strings.Repeat(" ", max(0, inner-lipgloss.Width(line)))+" "+side)
	}
	out = append(out, paintBorder(corners[2]+strings.Repeat(corners[4], max(0, width-2))+corners[3]))
	return strings.Join(out, "\n")
}

// sidebar lists every section and the current section's steps.
func (m *model) sidebar(height int) string {
	current := sectionOf(m.mode)
	var lines []string
	for _, s := range sections {
		if s.name != current.name {
			lines = append(lines, s.name)
			continue
		}
		lines = append(lines, m.paint("["+s.name+"]", accent, true))
		if len(s.steps) == 1 {
			continue
		}
		for _, step := range s.steps {
			if step == m.mode {
				lines = append(lines, "  "+m.paint("["+stepTitle(step)+"]", accent, true))
			} else {
				lines = append(lines, "  "+stepTitle(step))
			}
		}
	}
	return m.box("Sections", "", lines, sidebarWidth, height, false, accent)
}

// panelTitle is the main panel's title, its state and its tone.
func (m *model) panelTitle(f frame) (string, string, role) {
	scroll := scrollState(m.viewport)
	switch {
	case m.collectionReview != nil:
		return "Review collection acquisition", scroll, warning
	case m.reviewingDownload():
		return "Review acquisition", scroll, warning
	case m.confirmRoot != "":
		label := map[string]string{"init": "initialization", "import": "import", "rescan": "rescan", "cleanup": "cleanup", "restore": "restore", "purge": "purge"}[m.confirmKind]
		return "Review " + label, scroll, warning
	}
	title := stepTitle(m.mode)
	if f.side == 0 && len(sectionOf(m.mode).steps) > 1 {
		title = stepList(sectionOf(m.mode), m.mode)
	}
	var state []string
	switch {
	case m.busy:
		state = append(state, "working")
	case m.evidence:
		title += ": full report"
		state = append(state, scroll)
	case m.details && len(m.entries) > 0:
		title += ": details"
		state = append(state, scrollState(m.preview))
	case len(m.entries) > 0:
		state = append(state, fmt.Sprintf("%d of %d", m.selected+1, len(m.entries)))
	}
	if (m.mode == "search" || m.mode == "holdings") && m.total > 0 && !m.busy {
		state = append(state, fmt.Sprintf("%d matches", m.total))
	}
	if s := m.stepState(); s != "" {
		state = append(state, s)
	}
	if f.side == 0 {
		state = append(state, m.netWord())
	}
	state = slices.DeleteFunc(state, func(s string) bool { return s == "" })
	return title, strings.Join(state, "  "), accent
}

// scrollState is how far a pane has scrolled, or "" when it all fits.
func scrollState(v viewport.Model) string {
	if v.TotalLineCount() <= v.Height() {
		return ""
	}
	return fmt.Sprintf("scroll %.0f%%", v.ScrollPercent()*100)
}

// caveat is the step's standing limit, kept visible under its form. Keys and
// settings are in the footer and title instead.
func (m *model) caveat() string {
	switch m.mode {
	case "home":
		return "Offline and read-only. Scripts and agents: nemalo COMMAND --json, or nemalo mcp serve."
	case "search":
		if m.source == "gutenberg" {
			return "Local catalog; the Gutenberg website is not queried."
		}
	case "evaluate":
		return "Source declarations only. A Gutenberg catalog row requests its record on d."
	case "scan":
		return "Host cloud/sample policy applies; no remediation; not proof of safety."
	case "collections":
		return "Offline status. F8 previews acquisition before any request."
	case "revisions":
		return "Offline comparison. No holdings removed; no acquisition."
	case "read":
		if m.reader.asset != "" {
			return "Offline. Asset " + present.Exact(m.reader.asset) + ". Nothing is recorded; bookmarks only on m."
		}
		return "Select an asset in Holdings, then r."
	case "state":
		return "Writes happen only after a review and its own Enter."
	}
	return ""
}

// stepState is the step's current setting, shown in its panel title.
func (m *model) stepState() string {
	budget := "1 GiB"
	if m.largeBudget {
		budget = "5 GiB"
	}
	switch m.mode {
	case "search":
		name := map[string]string{"openlibrary": "Open Library", "archive": "Internet Archive", "gutenberg": "Gutenberg catalog"}[m.source]
		if m.source != "openlibrary" {
			name += ", order " + m.searchOrder()
		}
		return name
	case "holdings":
		return strings.ToUpper(m.format)
	case "inspect", "audit":
		return "budget " + budget
	case "snapshot":
		health := "off"
		if m.assess {
			health = "on"
		}
		return "budget " + budget + ", health " + health
	case "collections":
		if m.collectionLargeBudget {
			return "cap 5 GiB"
		}
		return "cap 1 GiB"
	}
	return ""
}

// panelView renders the frame and places the real cursor.
func (m *model) panelView() (string, *tea.Cursor) {
	f := m.layout()
	full := m.width - f.side
	var panels string
	cursorRow := -1
	focusedField := -1
	switch {
	case m.overlay != nil:
		lines, row := m.overlayLines(full-4, f.height-2)
		cursorRow = row
		panels = m.box(m.overlay.title(), fmt.Sprintf("%d shown", len(m.overlay.shown)), lines, full, f.height, true, accent)
	default:
		title, right, tone := m.panelTitle(f)
		var lines []string
		if !m.reviewing() {
			lines = m.formLayout(f.inner)
			for i, field := range m.formFields() {
				if field.focused {
					focusedField = i
				}
			}
			if c := m.caveat(); c != "" {
				for _, line := range strings.Split(lipgloss.Wrap(c, f.inner, " "), "\n") {
					lines = append(lines, m.paint(line, muted, false))
				}
			}
		}
		lines = append(lines, m.contentLines(f)...)
		focused := true
		panels = m.box(title, right, lines, f.mainW, f.height, focused, tone)
		if f.detailW > 0 {
			detail := strings.Split(m.preview.View(), "\n")
			panels = lipgloss.JoinHorizontal(lipgloss.Top, panels, m.box("Details", scrollState(m.preview), detail, f.detailW, f.height, false, accent))
		}
	}
	if f.side > 0 {
		panels = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(f.height), panels)
	}
	m.fieldRow, m.fieldCol = f.top+1, f.side+2
	view := strings.Join(f.header, "\n") + "\n" + panels + "\n" + m.statusLine() + "\n" + m.footer()
	var cursor *tea.Cursor
	switch {
	case m.overlay != nil:
		if cursorRow >= 0 {
			cursor = tea.NewCursor(f.side+2, f.top+1+cursorRow)
		}
	case focusedField >= 0 && !m.resultsFocused && !m.reviewing():
		for i, in := range []textinput.Model{m.input, m.secondary, m.intake} {
			if in.Focused() && m.fieldAt[i] >= 0 {
				cursor = inputCursor(in, m.fieldCol+m.fieldX[i], m.fieldRow+m.fieldAt[i], m.width)
			}
		}
	}
	return view, cursor
}

// contentLines is the main panel's content below the form.
func (m *model) contentLines(f frame) []string {
	switch {
	case m.busy && m.viewport.GetContent() == "":
		return []string{m.paint("Working. Escape cancels; Ctrl+C quits.", muted, false)}
	case m.viewport.GetContent() == "" && !m.busy && !m.reviewing():
		return strings.Split(lipgloss.Wrap(m.emptyHelp(), f.inner, " "), "\n")
	case m.evidence || len(m.entries) == 0 || m.reviewing():
		return strings.Split(m.viewport.View(), "\n")
	case m.details:
		return strings.Split(m.preview.View(), "\n")
	case m.mode == "home":
		return m.homeLines(f.inner, f.contentH)
	}
	return m.listRows(f.inner, f.contentH)
}

// homeLines puts Home's summary above its list of destinations.
func (m *model) homeLines(width, height int) []string {
	summary := strings.Split(strings.TrimRight(hardWrap(m.report, width), "\n"), "\n")
	room := height - len(summary) - 1
	if room < 6 {
		summary = summary[:max(0, min(len(summary), height-7))]
		room = height - len(summary) - 1
	}
	return append(append(summary, ""), m.listRows(width, max(3, room))...)
}

// listRows renders one page of results: a numbered title and a dim summary
// for each item, with > on the selected row.
func (m *model) listRows(width, height int) []string {
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
			title = m.paint(clip(title, width), accent, true)
		} else {
			title = clip(title, width)
		}
		rows = append(rows, title, m.paint(clip("      "+e.summary, width), muted, false), "")
	}
	return rows
}

// statusLine is the status, coloured by the operation's outcome.
func (m *model) statusLine() string {
	state := m.statusText()
	color := accent
	switch {
	case m.busy || m.reviewing():
		color = warning
	case m.outcome == outcomeFailed:
		color = danger
	case m.outcome == outcomeAttention:
		color = warning
	case m.outcome == outcomeOK:
		color = success
	}
	if m.overlay != nil {
		state = "Up and Down choose; Enter runs; Escape closes. Reviews still ask for Enter."
		if o := m.overlay; len(o.shown) > 0 && o.shown[o.selected].why != "" && o.kind != helpView {
			state = "Unavailable: " + o.shown[o.selected].why + "."
		}
		if m.overlay.kind == helpView {
			state = "Type to filter. Escape or ? closes."
		}
		color = accent
	}
	return lipgloss.Wrap(m.paint(state, color, false), max(1, m.width-2), " ")
}

// resizePanel sizes the report and preview to the panel frame.
func (m *model) resizePanel() {
	f := m.layout()
	full := m.width - f.side
	m.viewport.SetWidth(max(1, full-4))
	m.viewport.SetHeight(f.contentH)
	if m.reviewing() {
		m.viewport.SetHeight(max(1, f.height-2))
	}
	m.setReport(m.report)
	if f.detailW > 0 {
		m.preview.SetWidth(max(1, f.detailW-4))
		m.preview.SetHeight(max(1, f.height-2))
	} else {
		m.preview.SetWidth(max(1, full-4))
		m.preview.SetHeight(f.contentH)
	}
	m.refreshPreview()
}
