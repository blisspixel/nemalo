package present

import (
	"fmt"
	"strings"
)

// Human output reads as labelled values and sentences. Source text is shown
// without Go quoting but on one row with controls escaped (Line), fixed status
// tokens read as words, and empty values are left out. --json keeps every
// field and exact token.

// words shows fixed tokens such as not_resolved_at_file_level as words, also
// where a token leads a sentence, as in "file_synced; directory entry ...".
// Other text, such as a provider's free-text finding, is kept as it is, on
// one safe row.
func words(text string) string {
	fields := strings.Fields(Line(text))
	for i, f := range fields {
		token := strings.TrimRight(f, ";,.:")
		if strings.Contains(token, "_") && strings.Trim(token, "abcdefghijklmnopqrstuvwxyz0123456789_") == "" {
			fields[i] = strings.ReplaceAll(token, "_", " ") + f[len(token):]
		}
	}
	return strings.Join(fields, " ")
}

// list joins source values on one row. A semicolon separates them, because
// names such as "Austen, Jane" hold commas. It is "" for no values.
func list(values []string) string {
	shown := make([]string, 0, len(values))
	for _, v := range values {
		if v = Line(v); v != "" {
			shown = append(shown, v)
		}
	}
	return strings.Join(shown, "; ")
}

// field writes "label: value" on its own row, or nothing for an empty value.
// The value must already be safe for one row.
func field(b *strings.Builder, label, value string) {
	if value != "" {
		fmt.Fprintf(b, "%s: %s\n", label, value)
	}
}

// statusWithReason reads a status and its reason together, such as
// "review (unscanned)".
func statusWithReason(status, reason string) string {
	s := words(status)
	if reason != "" && reason != status {
		s += " (" + words(reason) + ")"
	}
	return s
}

// yes reads a flag as a word only when it is set.
func yes(set bool, word string) string {
	if set {
		return word
	}
	return ""
}

// joinSet joins the non-empty words with ", ".
func joinSet(values ...string) string {
	shown := values[:0:0]
	for _, v := range values {
		if v != "" {
			shown = append(shown, v)
		}
	}
	return strings.Join(shown, ", ")
}

// notes writes each fixed or source sentence on its own labelled row.
func notes(b *strings.Builder, label string, values []string) {
	for _, v := range values {
		field(b, label, words(v))
	}
}
