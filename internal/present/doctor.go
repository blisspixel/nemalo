package present

import (
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/app"
)

// Doctor summarizes the environment in a few rows: paths, configuration, the
// recommended scanner and its signatures, the descriptor bridge, optional
// tools, and anything that needs action. Nothing here executes a scanner or
// is scan evidence. --json has the full report.
func Doctor(d app.Doctor) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Nemalo doctor: %s, %s\n", Line(d.Platform), Line(d.Go))
	field(&b, "Config file", Exact(d.Paths.Config))
	field(&b, "State", Exact(d.Paths.State))
	field(&b, "Cache", Exact(d.Paths.Cache))
	field(&b, "Library", Exact(d.Config.Library))
	field(&b, "Review folder", Exact(d.Config.Review))
	field(&b, "Gutenberg catalog", Exact(d.Config.GutenbergCatalog))
	field(&b, "On detection", words(d.Config.OnDetection))
	var action []string
	optional := []string{}
	for _, c := range d.Capabilities {
		switch {
		case c.Recommended:
			name := scannerLabel(c.Name)
			if !c.Available {
				fmt.Fprintf(&b, "Scanner: %s is not installed. Scans are optional and recommended for a file you did not produce.\n", name)
				if d.ScannerSetup != "" {
					action = append(action, d.ScannerSetup)
				}
				continue
			}
			scanner := name + " is installed"
			if s := d.Signatures; s != nil {
				scanner += "; signatures " + words(s.Status)
				if s.Version != "" {
					scanner += ", version " + Line(s.Version)
				}
				if s.Updated != nil {
					scanner += ", updated " + s.Updated.UTC().Format("2006-01-02")
				}
				switch {
				case s.AgeDays == nil:
				case *s.AgeDays == 0:
					scanner += " (today)"
				default:
					scanner += fmt.Sprintf(" (%d day(s) ago)", *s.AgeDays)
				}
				if s.Status != "current" && s.Advice != "" {
					action = append(action, Line(s.Advice))
				}
			}
			field(&b, "Scanner", scanner)
		case c.Name == "descriptor_bridge":
			if c.Available {
				b.WriteString("Descriptor bridge: available\n")
			} else {
				b.WriteString("Descriptor bridge: unavailable. Library commands cannot reopen a library folder safely on this system.\n")
				action = append(action, "Run Nemalo where /proc/self/fd (Linux) or /dev/fd (macOS) is mounted.")
			}
		case c.Name == "clamscan" || c.Name == "MpCmdRun.exe":
			// The other platform's scanner is not a choice here.
		default:
			state := "not installed"
			if c.Available {
				state = "installed"
			}
			optional = append(optional, fmt.Sprintf("%s %s (%s)", Line(c.Name), state, words(strings.TrimPrefix(c.Use, "optional "))))
		}
	}
	field(&b, "Optional", strings.Join(optional, "; "))
	field(&b, "Checksums", Line(d.ChecksumRecommendation))
	for _, a := range action {
		fmt.Fprintf(&b, "Next: %s\n", a)
	}
	b.WriteString("Tool availability is not scan evidence. The full report is nemalo doctor --json.\n")
	return b.String()
}

func scannerLabel(name string) string {
	switch name {
	case "clamscan":
		return "ClamAV (clamscan)"
	case "MpCmdRun.exe":
		return "Microsoft Defender (MpCmdRun.exe)"
	}
	return Line(name)
}
