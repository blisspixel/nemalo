package tui

import "strings"

// Sections group steps by what people come to do (decision 0101). A step is
// one operation; its handlers are unchanged.
type section struct {
	name  string
	steps []string
}

var sections = []section{
	{"Discover", []string{"search", "evaluate"}},
	{"Library", []string{"holdings", "read"}},
	{"Collections", []string{"collections", "revisions"}},
	{"Check files", []string{"inspect", "check", "scan", "snapshot", "audit"}},
	{"Manage", []string{"state"}},
	{"Jobs", []string{"jobs"}},
}

var stepTitles = map[string]string{
	"search": "Search", "evaluate": "Evaluate", "holdings": "Holdings", "read": "Read",
	"collections": "Collections", "revisions": "Revisions", "inspect": "Inspect", "check": "Check",
	"scan": "Scan", "snapshot": "Snapshot", "audit": "Audit", "state": "Status", "jobs": "Jobs",
}

// steps is every step in section order, which Tab follows.
func steps() []string {
	var all []string
	for _, s := range sections {
		all = append(all, s.steps...)
	}
	return all
}

func sectionOf(step string) section {
	for _, s := range sections {
		for _, name := range s.steps {
			if name == step {
				return s
			}
		}
	}
	return sections[0]
}

func stepTitle(step string) string { return stepTitles[step] }

// stepShortcut is the step's Alt key, if it has one. The twelve original
// operations keep theirs; later steps are reached by Tab or the palette.
func stepShortcut(step string) string {
	for i, name := range modes {
		if name == step {
			return "alt+" + modeShortcut(i)
		}
	}
	return ""
}

// stepList names a section's steps, bracketing the current one so location
// never depends on colour.
func stepList(s section, current string) string {
	names := make([]string, len(s.steps))
	for i, step := range s.steps {
		names[i] = stepTitle(step)
		if step == current {
			names[i] = "[" + names[i] + "]"
		}
	}
	return strings.Join(names, "  ")
}

// usesNetwork reports whether the step's Enter contacts a provider.
func (m *model) usesNetwork() bool {
	return (m.mode == "search" && m.source != "gutenberg") || m.mode == "evaluate"
}
