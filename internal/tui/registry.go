package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/discovery"
)

// The action registry (decision 0101) is the one description of what a key
// does where. The footer, the Ctrl+O palette, the Space item menu and ? help
// read it; running an entry sends its key through the normal handlers, which
// stay the authority for availability and policy.

type focusScope int

const (
	anyFocus focusScope = iota
	formFocus
	listFocus
)

type effect int

const (
	local effect = iota
	// network contacts a provider when the key runs.
	network
	// review opens a write or network review that needs its own Enter.
	review
	// writes changes files without a review of its own.
	writes
	// scanner runs the installed antivirus, whose host policy applies.
	scans
)

type command struct {
	key  string // the key as tea.KeyPressMsg.String() spells it
	name string // what it does, for the palette and help
	// label is the footer's short wording; footer orders it (0: never shown).
	label  string
	footer int
	steps  []string // nil: every step
	focus  focusScope
	effect effect
	// nav moves to a step instead of sending a key.
	nav string
	// when explains why the action is unavailable now, or returns "".
	when func(*model) string
	// unprobed actions depend on content the parity test cannot fix, such as
	// a scroll position.
	unprobed bool
}

const (
	reasonBusy     = "Work is running; Escape cancels it"
	reasonNoItem   = "Select an item in the results first"
	reasonNoReport = "No report yet; run the step first"
)

func notBusy(m *model) string {
	if m.busy {
		return reasonBusy
	}
	return ""
}

func needs(reason string, ok func(*model) bool) func(*model) string {
	return func(m *model) string {
		if m.busy {
			return reasonBusy
		}
		if !ok(m) {
			return reason
		}
		return ""
	}
}

func hasEntries(m *model) bool { return len(m.entries) > 0 }

// browsing is a visible list, not an item's details or the full report.
func browsing(m *model) bool { return hasEntries(m) && !m.details && !m.evidence }

func selectedID(m *model) string {
	if len(m.entries) == 0 {
		return ""
	}
	return m.entries[m.selected].sourceID
}

func hasInput(m *model) bool { return m.input.Value() != "" }

func pageable(m *model) bool {
	return m.mode == "search" || m.mode == "holdings" || (m.mode == "evaluate" && discovery.ValidOpenLibraryWork(m.submittedInput))
}

// registry lists every action. Entries that share a key differ by step or
// focus; lookups take the first match.
var registry = buildRegistry()

func buildRegistry() []command {
	read := []string{"read"}
	cmds := []command{
		// Everywhere.
		{key: "ctrl+o", name: "Open the action palette", label: "actions", footer: 7, when: notBusy},
		{key: "ctrl+c", name: "Quit Nemalo", label: "quit", footer: 99, when: func(*model) string { return "" }},
		{key: "esc", name: "Go back, or cancel running work", label: "back", footer: 6, focus: listFocus, when: func(*model) string { return "" }},
		{key: "esc", name: "Cancel running work", label: "cancel", focus: formFocus, when: func(*model) string { return "" }},
		{key: "tab", name: "Go to the next step", label: "next step", footer: 8, when: notBusy},
		{key: "shift+tab", name: "Go to the previous step", when: notBusy},
		{key: "f6", name: "Browse the results", label: "results", footer: 5, focus: formFocus, when: notBusy},
		{key: "f6", name: "Return to the fields", label: "fields", focus: listFocus, when: notBusy},
		{key: "f5", name: "Show or hide the full report", label: "report", when: needs(reasonNoReport, func(m *model) bool { return m.report != "" })},
		{key: "pgup", name: "Scroll the report or details up", unprobed: true, when: func(*model) string { return "" }},
		{key: "pgdown", name: "Scroll the report or details down", unprobed: true, when: func(*model) string { return "" }},
		{key: "ctrl+n", name: "Move to the next field", label: "field", footer: 4, focus: anyFocus, steps: []string{"evaluate", "holdings", "read", "snapshot", "audit", "state", "collections", "revisions"}, when: notBusy},
		{key: "ctrl+n", name: "Move to the catalog field", label: "field", footer: 4, steps: []string{"search"}, when: needs("Only Gutenberg search has a catalog field", func(m *model) bool { return m.source == "gutenberg" })},

		// Results.
		{key: "up", name: "Select the previous item", label: "select", footer: 1, focus: listFocus, when: needs(reasonNoItem, func(m *model) bool { return browsing(m) && m.selected > 0 })},
		{key: "down", name: "Select the next item", label: "select", footer: 1, focus: listFocus, when: needs(reasonNoItem, func(m *model) bool { return browsing(m) && m.selected < len(m.entries)-1 })},
		{key: "home", name: "Select the first item on this page", focus: listFocus, when: needs(reasonNoItem, func(m *model) bool { return browsing(m) && m.selected > 0 })},
		{key: "end", name: "Select the last item on this page", focus: listFocus, when: needs(reasonNoItem, func(m *model) bool { return browsing(m) && m.selected < len(m.entries)-1 })},
		{key: "enter", name: "Go to the selected section or bookmark", label: "go", footer: 2, focus: listFocus, steps: []string{"home"}, when: needs("Select a section or bookmark", func(m *model) bool {
			return browsing(m) && (m.entries[m.selected].step != "" || m.entries[m.selected].bookmark != nil)
		})},
		{key: "enter", name: "Retrieve the selected unit", label: "read", footer: 2, focus: listFocus, steps: read, when: needs(reasonNoItem, hasEntries)},
		{key: "enter", name: "Show or hide the selected item's details", label: "details", footer: 2, focus: listFocus, when: needs(reasonNoItem, hasEntries)},
		{key: "space", name: "Actions for the selected item", label: "item actions", footer: 3, focus: listFocus, when: needs(reasonNoItem, hasEntries)},
		{key: "?", name: "Show help for every key", label: "help", footer: 4, focus: listFocus, when: notBusy},
		{key: ":", name: "Open the action palette", focus: listFocus, when: notBusy},
		{key: "L", name: "Show this session's messages", focus: listFocus, when: notBusy},
		{key: "/", name: "Filter this page as you type, offline", label: "filter", footer: 4, focus: listFocus, when: needs(reasonNoItem, func(m *model) bool { return hasEntries(m) || m.filter != nil })},
		{key: "y", name: "Copy the selected item's full identity", label: "copy", footer: 5, focus: listFocus, when: needs("This item has no identity to copy", func(m *model) bool { return hasEntries(m) && m.entries[m.selected].identity != "" })},
		{key: "ctrl+right", name: "Next results page", steps: []string{"search", "holdings", "evaluate"}, unprobed: true, when: needs("Only search, holdings and Open Library editions page", pageable)},
		{key: "ctrl+left", name: "Previous results page", steps: []string{"search", "holdings", "evaluate"}, unprobed: true, when: needs("Only search, holdings and Open Library editions page", pageable)},

		// Home.
		{key: "enter", name: "Read the library overview", label: "overview", footer: 1, focus: formFocus, steps: []string{"home"}, when: needs("Enter a library folder first", hasInput)},

		// Discover.
		{key: "enter", name: "Search the selected provider", label: "search", footer: 1, focus: formFocus, steps: []string{"search"}, effect: network, when: needs("Type a query first", hasInput)},
		{key: "f2", name: "Change the search provider", label: "provider", footer: 2, steps: []string{"search"}, when: notBusy},
		{key: "f3", name: "Change the result order", label: "order", footer: 3, steps: []string{"search"}, when: needs("Only Internet Archive and Gutenberg searches have an order", func(m *model) bool { return m.source == "archive" || m.source == "gutenberg" })},
		{key: "e", name: "Prepare evaluation of the selected result", label: "evaluate", footer: 3, focus: listFocus, steps: []string{"search"}, when: needs("Select an Archive, Gutenberg, Open Library work or OpenAlex work result", func(m *model) bool {
			id := selectedID(m)
			return strings.HasPrefix(id, "archive:") || strings.HasPrefix(id, "gutenberg:") || discovery.ValidOpenLibraryWork(id) || discovery.ValidOpenAlexWork(id)
		})},
		{key: "enter", name: "Evaluate the item", label: "evaluate", footer: 1, focus: formFocus, steps: []string{"evaluate"}, effect: network, when: needs("Enter an item ID first", hasInput)},
		{key: "e", name: "Select this edition", label: "edition", footer: 3, focus: listFocus, steps: []string{"evaluate"}, when: needs("Select an Open Library edition", func(m *model) bool { return discovery.ValidOpenLibraryEdition(selectedID(m)) })},
		{key: "e", name: "Select this location's identifier", label: "identifier", footer: 3, focus: listFocus, steps: []string{"evaluate"}, when: needs("Select an OpenAlex location stating a pmc: or arxiv: identifier", func(m *model) bool {
			return m.lastEvaluation != nil && m.lastEvaluation.OpenAccess != nil && locationIdentifier(selectedID(m))
		})},
		{key: "d", name: "Review acquiring the selected file", label: "acquire", footer: 3, focus: listFocus, steps: []string{"evaluate"}, effect: review, when: func(m *model) string {
			if m.lastEvaluation != nil && m.lastEvaluation.OpenAccess != nil {
				return "An OpenAlex work is discovery only; evaluate a location's pmc: or arxiv: identifier first"
			}
			return needs(reasonNoItem, hasEntries)(m)
		}},
		{key: "a", name: "Review acquiring every track of the recording", focus: listFocus, steps: []string{"evaluate"}, effect: review, when: needs("Select a track of an acquirable recording", func(m *model) bool { return hasEntries(m) && m.recordingFor(selectedID(m)) != nil })},

		// Library.
		{key: "enter", name: "Load holdings", label: "load", footer: 1, focus: formFocus, steps: []string{"holdings"}, when: needs("Enter a catalog or library folder first", hasInput)},
		{key: "f4", name: "Change the format filter", label: "format", footer: 2, steps: []string{"holdings"}, when: notBusy},
		{key: "r", name: "Read the selected asset", label: "read", footer: 3, focus: listFocus, steps: []string{"holdings"}, when: needs(reasonNoItem, hasEntries)},
		{key: "o", name: "Open a verified copy in the default application", label: "open", focus: listFocus, steps: []string{"holdings"}, when: needs("Select a holding of a library folder", func(m *model) bool { return hasEntries(m) && m.entries[m.selected].managed })},
		{key: "t", name: "Show the recording that holds this track", focus: listFocus, steps: []string{"holdings"}, when: needs("Select a track of a recording", func(m *model) bool { return hasEntries(m) && m.entries[m.selected].recording != "" })},
		{key: "enter", name: "List the asset's source units", label: "units", footer: 1, focus: formFocus, steps: read, when: notBusy},
		{key: "f2", name: "Show recorded citation metadata", label: "citation", footer: 2, steps: read, when: notBusy},
		{key: "f4", name: "Show evidence for the last excerpt", label: "evidence", footer: 3, steps: read, when: notBusy},
		{key: "n", name: "Continue with the next range", focus: listFocus, steps: read, when: needs("No continuation yet", func(m *model) bool { return m.reader.next != "" })},
		{key: "r", name: "Repeat the last request", focus: listFocus, steps: read, when: notBusy},
		{key: "u", name: "List source units", focus: listFocus, steps: read, when: notBusy},
		{key: "c", name: "List the declared contents", focus: listFocus, steps: read, when: notBusy},
		{key: "g", name: "List the declared page labels", focus: listFocus, steps: read, when: notBusy},
		{key: "p", name: "Open as pages", focus: listFocus, steps: read, when: notBusy},
		{key: "s", name: "Open one word at a time", focus: listFocus, steps: read, when: notBusy},
		{key: "m", name: "Save a bookmark", focus: listFocus, steps: read, effect: writes, when: notBusy},
		{key: "'", name: "Restore the saved bookmark", focus: listFocus, steps: read, when: notBusy},

		// Collections.
		{key: "enter", name: "Read collection status", label: "status", footer: 1, focus: formFocus, steps: []string{"collections"}, when: needs("Enter a collection file first", hasInput)},
		{key: "f8", name: "Review the collection acquisition", label: "acquire", footer: 2, steps: []string{"collections"}, effect: review, when: notBusy},
		{key: "f3", name: "Change the transfer cap", label: "cap", footer: 3, steps: []string{"collections"}, when: notBusy},
		{key: "enter", name: "Compare the revisions", label: "compare", footer: 1, focus: formFocus, steps: []string{"revisions"}, when: needs("Enter the earlier collection file first", hasInput)},

		// Check files.
		{key: "enter", name: "Inspect the folder", label: "inspect", footer: 1, focus: formFocus, steps: []string{"inspect"}, when: needs("Enter a folder first", hasInput)},
		{key: "enter", name: "Check the file", label: "check", footer: 1, focus: formFocus, steps: []string{"check"}, when: needs("Enter a file first", hasInput)},
		{key: "enter", name: "Scan the file with the installed antivirus", label: "scan", footer: 1, focus: formFocus, steps: []string{"scan"}, effect: scans, when: needs("Enter a file first", hasInput)},
		{key: "enter", name: "Write a snapshot catalog", label: "snapshot", footer: 1, focus: formFocus, steps: []string{"snapshot"}, effect: writes, when: needs("Enter a folder first", hasInput)},
		{key: "enter", name: "Audit the catalog or check the library", label: "audit", footer: 1, focus: formFocus, steps: []string{"audit"}, when: needs("Enter a catalog or library folder first", hasInput)},
		{key: "f3", name: "Change the read budget", label: "budget", footer: 2, steps: []string{"inspect", "snapshot", "audit"}, when: notBusy},
		{key: "ctrl+a", name: "Turn health metadata on or off", label: "health", footer: 3, steps: []string{"snapshot"}, when: notBusy},

		// Manage.
		{key: "enter", name: "Read library status", label: "status", footer: 1, focus: formFocus, steps: []string{"state"}, when: needs("Enter a library folder first", hasInput)},
		{key: "f7", name: "Review library initialization", label: "initialize", footer: 2, steps: []string{"state"}, effect: review, when: notBusy},
		{key: "f8", name: "Review an import", label: "import", footer: 2, steps: []string{"state"}, effect: review, when: notBusy},
		{key: "f9", name: "Review a cleanup, or a restore of a cleanup ID", label: "cleanup", footer: 3, steps: []string{"state"}, effect: review, when: notBusy},
		{key: "ctrl+r", name: "Review a rescan", steps: []string{"state"}, effect: review, when: notBusy},
		{key: "ctrl+p", name: "Review a purge of a cleanup ID", steps: []string{"state"}, effect: review, when: notBusy},
		{key: "f10", name: "Show recorded jobs", label: "jobs", footer: 3, steps: []string{"state", "jobs"}, when: notBusy},

		// Jobs.
		{key: "enter", name: "Observe recorded jobs", label: "observe", footer: 1, focus: formFocus, steps: []string{"jobs"}, when: needs("Enter a library folder first", hasInput)},
	}
	for _, s := range sections {
		for _, step := range s.steps {
			target := step
			cmds = append(cmds, command{key: stepShortcut(step), name: "Go to " + s.name + ": " + stepTitle(step), nav: step, when: func(m *model) string {
				if m.mode == target {
					return "This is the current step"
				}
				return notBusy(m)
			}})
		}
	}
	return cmds
}

// applies reports whether the command belongs to the current step and focus.
func (c command) applies(m *model) bool {
	if c.nav != "" {
		return true
	}
	if c.focus == formFocus && m.resultsFocused || c.focus == listFocus && !m.resultsFocused {
		return false
	}
	return c.inStep(m.mode)
}

func (c command) inStep(step string) bool {
	if c.steps == nil {
		return true
	}
	for _, s := range c.steps {
		if s == step {
			return true
		}
	}
	return false
}

// lookup is the registry entry for a key in the current step and focus.
func (m *model) lookup(key string) (command, bool) {
	for _, c := range registry {
		if c.key == key && c.applies(m) {
			return c, true
		}
	}
	return command{}, false
}

// effectOf is the command's effect here: Gutenberg search reads a local catalog.
func (m *model) effectOf(c command) effect {
	if c.effect == network && c.key == "enter" && c.inStep("search") && len(c.steps) == 1 && m.source == "gutenberg" {
		return local
	}
	return c.effect
}

func effectTag(e effect) string {
	switch e {
	case network:
		return "network"
	case review:
		return "review"
	case writes:
		return "writes"
	case scans:
		return "scanner"
	}
	return ""
}

// footerKeys is the focused context's most useful actions, at most six,
// ending with quit. Reviews and running work have their own fixed keys.
func (m *model) footerKeys() []command {
	// Candidates in footer order; the registry order breaks ties.
	var candidates []command
	for _, c := range footerOrder {
		if !c.applies(m) || m.resultsFocused && c.focus != listFocus && c.key != "ctrl+c" {
			continue
		}
		candidates = append(candidates, c)
	}
	var picked []command
	seen := map[string]bool{}
	for _, c := range candidates {
		if len(picked) == 6 {
			break
		}
		if seen[c.label] || seen[c.key] || c.key != "ctrl+c" && len(picked) >= 5 || c.when(m) != "" {
			continue
		}
		picked = append(picked, c)
		seen[c.label], seen[c.key] = true, true
	}
	return picked
}

// footerOrder is the registry's footer entries by footer priority.
var footerOrder = func() []command {
	var order []command
	for _, c := range registry {
		if c.footer > 0 {
			order = append(order, c)
		}
	}
	slices.SortStableFunc(order, func(a, b command) int { return a.footer - b.footer })
	return order
}()

func footerKey(c command) string {
	switch c.key {
	case "up", "down":
		return "Up/Down"
	case "space":
		return "Space"
	}
	return keyLabel(c.key)
}

func keyLabel(key string) string {
	if key == "" {
		return ""
	}
	if len(key) == 1 {
		return key
	}
	parts := strings.Split(key, "+")
	for i, p := range parts {
		switch p {
		case "esc":
			p = "Esc"
		case "pgup":
			p = "PgUp"
		case "pgdown":
			p = "PgDown"
		default:
			p = strings.ToUpper(p[:1]) + p[1:]
		}
		parts[i] = p
	}
	return strings.Join(parts, "+")
}

var namedKeys = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "space": tea.KeySpace,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
	"f1": tea.KeyF1, "f2": tea.KeyF2, "f3": tea.KeyF3, "f4": tea.KeyF4, "f5": tea.KeyF5, "f6": tea.KeyF6,
	"f7": tea.KeyF7, "f8": tea.KeyF8, "f9": tea.KeyF9, "f10": tea.KeyF10, "f11": tea.KeyF11, "f12": tea.KeyF12,
	"backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
}

// keyPress builds the key press a registry key names, so running an action
// reaches the same handler as typing it.
func keyPress(key string) tea.KeyPressMsg {
	var mod tea.KeyMod
	for {
		switch {
		case strings.HasPrefix(key, "ctrl+"):
			mod |= tea.ModCtrl
			key = key[len("ctrl+"):]
			continue
		case strings.HasPrefix(key, "alt+"):
			mod |= tea.ModAlt
			key = key[len("alt+"):]
			continue
		case strings.HasPrefix(key, "shift+"):
			mod |= tea.ModShift
			key = key[len("shift+"):]
			continue
		}
		break
	}
	if code, ok := namedKeys[key]; ok {
		k := tea.Key{Code: code, Mod: mod}
		if code == tea.KeySpace && mod == 0 {
			k.Text = " "
		}
		return tea.KeyPressMsg(k)
	}
	r := []rune(key)[0]
	k := tea.Key{Code: r, Mod: mod}
	if mod == 0 {
		k.Text = key
	}
	return tea.KeyPressMsg(k)
}
