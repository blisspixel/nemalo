package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/blisspixel/nemalo/internal/present"
)

// A detail card (decision 0102) has one shape for every item: a title, one
// line of facts, status chips, a short identity, aligned lists and one dim
// line of caveats. Each fact appears once; the full report (F5) keeps every
// recorded field and limitation.

type chip struct {
	text string
	role role
}

type cardList struct {
	label  string
	values []string
}

type card struct {
	title    string
	facts    []string
	chips    []chip
	identity string
	lists    []cardList
	caveat   string
	keys     string
}

// render writes the card. Values must already be safe for one row.
func (m *model) render(c card) string {
	var b strings.Builder
	b.WriteString(c.title + "\n")
	if facts := joinFacts(c.facts); facts != "" {
		b.WriteString(facts + "\n")
	}
	if len(c.chips) > 0 {
		parts := make([]string, len(c.chips))
		for i, ch := range c.chips {
			parts[i] = m.paint(ch.text, ch.role, ch.role != muted)
		}
		b.WriteString("\n" + strings.Join(parts, "   ") + "\n")
	}
	width := len("Identity")
	for _, l := range c.lists {
		width = max(width, len(l.label))
	}
	b.WriteString("\n")
	if c.identity != "" {
		fmt.Fprintf(&b, "%-*s  %s   %s\n", width, "Identity", shortID(c.identity), m.paint("y copies", muted, false))
	}
	for _, l := range c.lists {
		for i, v := range l.values {
			label := ""
			if i == 0 {
				label = l.label
			}
			fmt.Fprintf(&b, "%-*s  %s\n", width, label, v)
		}
	}
	caveat := strings.TrimSpace(c.caveat + "  F5 full report")
	b.WriteString("\n" + m.paint(caveat, muted, false) + "\n")
	if c.keys != "" {
		b.WriteString(m.paint(c.keys, muted, false) + "\n")
	}
	return b.String()
}

func joinFacts(facts []string) string {
	var kept []string
	for _, f := range facts {
		if f != "" {
			kept = append(kept, f)
		}
	}
	return strings.Join(kept, "   ")
}

func nonEmpty(values ...string) []string {
	var kept []string
	for _, v := range values {
		if v != "" {
			kept = append(kept, v)
		}
	}
	return kept
}

// healthChip is a recorded assessment status, from its typed value.
func healthChip(status string) chip {
	switch status {
	case "limited_checks_passed":
		return chip{"Limited checks passed", success}
	case "needs_review":
		return chip{"Needs review", warning}
	case "incomplete":
		return chip{"Checks incomplete", warning}
	case "invalid_or_unsupported":
		return chip{"Invalid or unsupported", danger}
	case "":
		return chip{"Not assessed", muted}
	}
	return chip{present.Words(status), warning}
}

// antivirusChip never turns a missing or partial scan into a clean one.
func antivirusChip(status string) chip {
	switch status {
	case "no_detections_reported":
		return chip{"No detections reported", success}
	case "not_scanned", "":
		return chip{"Not scanned", muted}
	case "findings":
		return chip{"Detection", danger}
	}
	return chip{"Scan " + present.Words(status), warning}
}

// holdingChip is a managed holding's recorded status and reason.
func holdingChip(status, reason string) chip {
	text := present.Words(status)
	if text != "" {
		text = strings.ToUpper(text[:1]) + text[1:]
	}
	if reason != "" && reason != status {
		text += ": " + present.Words(reason)
	}
	switch status {
	case "checked":
		return chip{text, success}
	case "review":
		return chip{text, warning}
	}
	return chip{text, muted}
}

// jobChip is a recorded job outcome; running means recorded, not observed.
func jobChip(status string) chip {
	switch status {
	case "succeeded":
		return chip{"Recorded succeeded", success}
	case "failed", "interrupted":
		return chip{"Recorded " + present.Words(status), danger}
	case "cancelled":
		return chip{"Recorded cancelled", muted}
	}
	return chip{"Recorded " + present.Words(status), warning}
}

// copyIdentity sends the selected item's full identity to the terminal
// clipboard (OSC 52). A terminal may ignore that request without telling
// the program, so the status says so instead of claiming success.
func (m *model) copyIdentity() tea.Cmd {
	if len(m.entries) == 0 || m.entries[m.selected].identity == "" {
		m.status = "This item has no identity to copy."
		m.outcome = outcomeAttention
		m.resize()
		return nil
	}
	id := m.entries[m.selected].identity
	m.status = "Sent " + shortID(id) + " to the terminal clipboard (OSC 52). A terminal without OSC 52 ignores it; F5 shows the full value."
	m.resize()
	return tea.SetClipboard(id)
}

// labelledChips makes dim "label value" chips, leaving out empty values.
func labelledChips(pairs ...string) []chip {
	var chips []chip
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			chips = append(chips, chip{pairs[i] + " " + pairs[i+1], muted})
		}
	}
	return chips
}
