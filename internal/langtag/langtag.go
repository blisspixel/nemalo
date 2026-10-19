// Package langtag reads declared language codes as BCP 47 tags, so a filter
// can match a base language across forms: "en", "en-US", and the ISO 639-2
// "eng" share the base "en". It never infers a language from text or names.
package langtag

import (
	"strings"

	"golang.org/x/text/language"
)

// Base returns the base language of a well-formed tag or ISO 639 code. Free
// text such as "English", "und", private-use tags, and anything that does not
// parse exactly give false: unknown is not guessed.
func Base(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 100 {
		return "", false
	}
	tag, err := language.Parse(raw)
	if err != nil {
		return "", false
	}
	base, confidence := tag.Base()
	if confidence != language.Exact || base.String() == "und" {
		return "", false
	}
	return base.String(), true
}

// Match reports whether any declared value has the base language want, and
// whether any declared value had a readable base at all.
func Match(declared []string, want string) (matched, known bool) {
	for _, d := range declared {
		if base, ok := Base(d); ok {
			known = true
			if base == want {
				return true, true
			}
		}
	}
	return false, known
}
