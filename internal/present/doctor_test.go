package present

import (
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/scanner"
)

func TestDoctorIsAShortSummaryThatNamesWhatNeedsAction(t *testing.T) {
	days, updated := 12, time.Date(2026, 10, 6, 6, 0, 0, 0, time.UTC)
	stale := app.Doctor{Platform: "linux/amd64", Go: "go1.27.2", Paths: config.Paths{Config: "/c/config.json", State: "/s", Cache: "/k"}, Config: config.Config{Library: "/srv/My Books"},
		Capabilities: []app.Capability{{Name: "clamscan", Available: true, Recommended: true}, {Name: "MpCmdRun.exe"}, {Name: "7z", Use: "optional future archive adapter"}, {Name: "descriptor_bridge", Available: true}},
		Signatures:   &scanner.Signatures{Tool: "clamav", Status: "stale", Version: "28000", Updated: &updated, AgeDays: &days, Advice: "Update signatures with freshclam."}}
	out := Doctor(stale)
	for _, want := range []string{"Nemalo doctor: linux/amd64, go1.27.2\n", "Library: /srv/My Books\n", "Scanner: ClamAV (clamscan) is installed; signatures stale, version 28000, updated 2026-10-06 (12 day(s) ago)\n", "Descriptor bridge: available\n", "Optional: 7z not installed (future archive adapter)\n", "Next: Update signatures with freshclam.\n", "nemalo doctor --json"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "MpCmdRun") || strings.Contains(out, "{") || strings.Count(out, "\n") > 14 {
		t.Fatal("not a short summary for this platform", out)
	}
	missing := app.Doctor{Platform: "linux/amd64", ScannerSetup: "Install ClamAV: sudo pacman -S clamav",
		Capabilities: []app.Capability{{Name: "clamscan", Recommended: true}, {Name: "descriptor_bridge"}}}
	out = Doctor(missing)
	for _, want := range []string{"Scanner: ClamAV (clamscan) is not installed.", "Next: Install ClamAV: sudo pacman -S clamav\n", "Descriptor bridge: unavailable.", "Next: Run Nemalo where /proc/self/fd"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "Review folder") || strings.Contains(out, "Gutenberg catalog") {
		t.Fatal("empty configuration shown", out)
	}
}
