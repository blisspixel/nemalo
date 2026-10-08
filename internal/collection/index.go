package collection

import (
	"fmt"
	"html"
	"io"
	"net/url"
	"strings"
)

// WriteIndex creates a portable reading list. Links are relative to the intake
// parent; generating the list never opens a reader or executes acquired content.
func WriteIndex(w io.Writer, m Manifest, report Report, intakePrefix string) error {
	var out strings.Builder
	fmt.Fprintf(&out, "# Nemalo validation collection\n\n%d of %d selected resources acquired.\n\n", report.AcquiredResources, len(m.Resources))
	out.WriteString("Intake is untrusted. Hashes and container checks are not a safety guarantee. See the dated acquisition and any external scanner reports before using files.\n\n")
	results := map[string]Result{}
	for _, r := range report.Results {
		results[r.ResourceID] = r
	}
	for _, r := range m.Resources {
		fmt.Fprintf(&out, "## %s\n\n%s | %s | %s\n\n%s\n\n", markdown(r.Title), r.ID, r.Type, markdown(strings.Join(r.Languages, ", ")), markdown(r.Rationale))
		fmt.Fprintf(&out, "Authors: %s\n\nRights: %s\n\n", markdown(strings.Join(r.Authors, "; ")), markdown(r.Rights))
		if r.Notes != "" {
			fmt.Fprintf(&out, "Notes: %s\n\n", markdown(r.Notes))
		}
		result, ok := results[r.ID]
		if !ok || !result.Acquired {
			out.WriteString("Acquisition incomplete. Consult the JSON report.\n\n")
		}
		for _, a := range result.Assets {
			u := (&url.URL{Path: strings.TrimSuffix(intakePrefix, "/") + "/" + r.ID + "/" + a.Asset.Name}).String()
			fmt.Fprintf(&out, "- [%s](%s): %d bytes, SHA-256 `%s`\n", markdown(a.Asset.Name), u, a.Bytes, a.SHA256)
		}
		out.WriteString("\n")
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func markdown(s string) string {
	s = html.EscapeString(s)
	return strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "#", "\\#", "\r", " ", "\n", " ").Replace(s)
}
