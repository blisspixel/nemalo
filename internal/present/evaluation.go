package present

import (
	"fmt"
	"sort"
	"strings"

	"github.com/blisspixel/nemalo/internal/discovery"
)

func Evaluation(e discovery.Evaluation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Metadata evaluation complete: %t\n", e.Complete)
	fmt.Fprintf(&b, "Item: %q\nTitles: %q\nCreators: %q\nLanguages: %q\nMedia types: %q\nDates: %q\nPublishers: %q\nRights statements: %q\nDeclared license URLs: %q\nAccess: %q\nPage: %q\nMetadata SHA-256: %q\nFiles listed: %d\n", e.ID, e.Title, e.Creators, e.Languages, e.MediaType, e.Dates, e.Publishers, e.Rights, e.LicenseURLs, e.Access, e.LandingPage, e.MetadataSHA256, len(e.Files))
	keys := make([]string, 0, len(e.Restrictions))
	for key := range e.Restrictions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "Restriction evidence: %q = %q\n", key, e.Restrictions[key])
	}
	for i, f := range e.Files {
		fmt.Fprintf(&b, "\n%d. %q [%q; %q]\n   Source format: %q\n", i+1, f.Name, f.CandidateKind, f.Access, f.Format)
		if f.Bytes != nil {
			fmt.Fprintf(&b, "   Declared bytes: %d\n", *f.Bytes)
		} else {
			b.WriteString("   Declared bytes: unknown\n")
		}
		fmt.Fprintf(&b, "   Private flag: %q; restricted flag: %q\n   File rights: %q; DRM: %q\n", f.PrivateState, f.RestrictionState, f.RightsStatus, f.DRMStatus)
		if f.MD5 != "" {
			fmt.Fprintf(&b, "   Source MD5: %q\n", f.MD5)
		}
		if f.SHA1 != "" {
			fmt.Fprintf(&b, "   Source SHA-1: %q\n", f.SHA1)
		}
		if f.DownloadURL != "" {
			fmt.Fprintf(&b, "   Candidate URL: %q\n", f.DownloadURL)
		}
		for _, finding := range f.Findings {
			fmt.Fprintf(&b, "   Finding: %q\n", finding)
		}
	}
	for _, limitation := range e.Limitations {
		fmt.Fprintf(&b, "Limit: %q\n", limitation)
	}
	return b.String()
}
