package textsafe

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode"
)

func TestJSONRoundTripAndVisibleControls(t *testing.T) {
	payload := "日本語 العربية Ελληνικά café\x1b\a\b\x7f\u009b\u009d\u202e\u2066\u2069\u200c\u200d\U000e0001 literal \\u009b\n\t"
	want := map[string]string{payload: payload}
	for _, indent := range []bool{false, true} {
		var out bytes.Buffer
		if err := WriteJSON(&out, want, indent); err != nil {
			t.Fatal(err)
		}
		for _, r := range out.String() {
			if unicode.Is(unicode.Cf, r) || (unicode.IsControl(r) && r != '\n' && r != '\t') {
				t.Fatalf("raw sensitive code point %U", r)
			}
		}
		var got map[string]string
		if err := json.Unmarshal(out.Bytes(), &got); err != nil || got[payload] != payload || len(got) != 1 {
			t.Fatal("parsed data changed", got, err)
		}
		if !strings.Contains(out.String(), "日本語 العربية Ελληνικά café") || !strings.HasSuffix(out.String(), "\n") {
			t.Fatal("ordinary text or framing lost", out.String())
		}
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestJSONErrors(t *testing.T) {
	if err := WriteJSON(io.Discard, make(chan int), false); err == nil {
		t.Fatal("unsupported value accepted")
	}
	if err := WriteJSON(shortWriter{}, "x", true); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	if err := WriteJSON(failedWriter{}, "x", false); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	if got := Visible("a\x1b\u009b\u202eb\n\t"); got != "a\\u001b\\u009b\\u202eb\n\t" {
		t.Fatal(got)
	}
}
