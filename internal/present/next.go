package present

import (
	"runtime"
	"strings"
	"unicode"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

// Next steps name the command that continues the first-book path: evaluate,
// acquire, import, then read. They are suggestions printed for the person to
// run; nothing is fetched or written until they do.

// ShellArg quotes s for pasting into a POSIX shell, or PowerShell on Windows,
// so no character in it is interpreted. ok is false when s holds a control or
// format character, which a terminal cannot show for copying.
func ShellArg(s string) (string, bool) {
	if s == "" || textsafe.Visible(s) != s {
		return "", false
	}
	safe := true
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return "", false
		}
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-:+@%=,", r) || (r == '\\' && runtime.GOOS == "windows")) {
			safe = false
			break
		}
	}
	switch {
	case safe:
		return s, true
	case runtime.GOOS == "windows":
		return "'" + strings.ReplaceAll(s, "'", "''") + "'", true
	default:
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'", true
	}
}

// command joins words, quoting each argument. It is "" when an argument
// cannot be shown for copying.
func command(words ...string) string {
	out := make([]string, len(words))
	for i, w := range words {
		if isPlaceholder(w) {
			out[i] = w
			continue
		}
		quoted, ok := ShellArg(w)
		if !ok {
			return ""
		}
		out[i] = quoted
	}
	return strings.Join(out, " ")
}

// isPlaceholder reports an uppercase word the person replaces, such as
// LIBRARY or NEW_DIRECTORY.
func isPlaceholder(w string) bool {
	return w != "" && strings.Trim(w, "ABCDEFGHIJKLMNOPQRSTUVWXYZ_") == ""
}

// NextAfterEvaluation names the acquire command for the first eligible EPUB,
// else the first eligible PDF, as acquire would accept it from this metadata.
// A local Gutenberg catalog row lists no files, so it names the record
// request. It is "" when no file is eligible.
func NextAfterEvaluation(e discovery.Evaluation, eligible []acquisition.Alternative) string {
	if e.Source == "gutenberg" && e.Access == "local_catalog" && len(e.Files) == 0 {
		if c := command("nemalo", "evaluate", e.ID, "--files"); c != "" {
			return "Next: " + c + "\n"
		}
		return ""
	}
	for _, want := range []string{"epub", "pdf"} {
		for _, a := range eligible {
			if a.Format != want {
				continue
			}
			if c := command("nemalo", "acquire", e.ID, "--file", a.File, "--output", "NEW_DIRECTORY"); c != "" {
				return "Next: " + c + "\n"
			}
		}
	}
	return ""
}

// NextAfterAcquisition names the import of a complete, undetected download.
// libraryRoot is the configured library, or "" for a placeholder.
func NextAfterAcquisition(r acquisition.Result, libraryRoot string) string {
	if !r.Complete || strings.HasPrefix(r.Status, "detected_") || r.Output == "" {
		return ""
	}
	if libraryRoot == "" {
		libraryRoot = "LIBRARY"
	}
	c := command("nemalo", "library", "import", libraryRoot, r.Output, "--apply")
	if c == "" {
		return ""
	}
	next := "Next: " + c + "\n"
	if libraryRoot == "LIBRARY" {
		next += "If you have no library yet, create an empty folder and run: nemalo library init LIBRARY\n"
	}
	return next
}

// NextAfterImport names the reader for a held EPUB or PDF, or the apply step
// after a preview.
func NextAfterImport(r library.ImportResult) string {
	held := r.AssetID != "" && (r.Applied || r.AlreadyHeld)
	switch {
	case !r.Applied && !r.AlreadyHeld && r.AssetID != "":
		// The apply repeats the preview's scan choice and names its plan, so
		// it writes only what this preview showed.
		words := []string{"nemalo", "library", "import", r.Library, r.Source, "--apply"}
		if r.ScanRequested {
			words = append(words, "--scan")
		}
		if r.Plan != "" {
			words = append(words, "--plan", r.Plan)
		}
		if c := command(words...); c != "" {
			return "Next: " + c + "\n"
		}
	case held && (r.Format == "epub" || r.Format == "pdf"):
		if c := command("nemalo", "read", r.Library, "--root", r.Library, "--asset", r.AssetID); c != "" {
			return "Next: " + c + "\n"
		}
	case held:
		if c := command("nemalo", "library", "open", r.Library, "--asset", r.AssetID); c != "" {
			return "Next: " + c + "\n"
		}
	}
	return ""
}
