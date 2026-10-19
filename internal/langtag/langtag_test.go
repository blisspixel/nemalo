package langtag

import "testing"

func TestBaseReadsTagsAndCodesWithoutGuessing(t *testing.T) {
	for raw, want := range map[string]string{
		"en": "en", "en-US": "en", "eng": "en", " EN-gb ": "en", "fre": "fr", "fra": "fr",
		"ger": "de", "deu": "de", "zh-Hant-TW": "zh", "chi": "zh", "jpn": "ja", "iw": "he",
		"nob": "nb", "es-419": "es", "pt-BR": "pt",
	} {
		if got, ok := Base(raw); !ok || got != want {
			t.Fatal(raw, got, ok)
		}
	}
	for _, raw := range []string{"", "English", "und", "x-klingon", "en_US!", string(make([]byte, 101))} {
		if got, ok := Base(raw); ok {
			t.Fatal("guessed", raw, got)
		}
	}
	if m, k := Match([]string{"English", "eng"}, "en"); !m || !k {
		t.Fatal("eng not matched")
	}
	if m, k := Match([]string{"fr"}, "en"); m || !k {
		t.Fatal("fr matched en")
	}
	if m, k := Match([]string{"English", ""}, "en"); m || k {
		t.Fatal("free text was read as a language")
	}
}
