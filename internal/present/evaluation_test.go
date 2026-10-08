package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/discovery"
)

func TestEvaluationDoesNotEmitTerminalControls(t *testing.T) {
	n := int64(10)
	e := discovery.Evaluation{ID: "archive:demo\x1b", Title: []string{"Title\x00"}, Rights: []string{"rights\x1b"}, LicenseURLs: []string{"javascript:untrusted\x1b"}, Restrictions: map[string]string{"z\x1b": "true\x00", "a": "unknown"}, Files: []discovery.OfferedFile{{Name: "file\x1b.epub", Format: "EPUB\x00", CandidateKind: "kind\x1b", Bytes: &n, Access: "uncertain\x00", DownloadURL: "url\x1b", MD5: "md5\x1b", SHA1: "sha1\x00", PrivateState: "private\x1b", DRMStatus: "unknown\x00", Findings: []string{"finding\x00"}}, {Name: "unknown.pdf"}}, Limitations: []string{"not verified\x1b"}}
	out := Evaluation(e)
	if strings.ContainsAny(out, "\x00\x1b") || !strings.Contains(out, "Declared bytes: unknown") || !strings.Contains(out, "Declared bytes: 10") || !strings.Contains(out, "Rights statements:") || !strings.Contains(out, "not verified") {
		t.Fatal(out)
	}
	if strings.Index(out, `"a" =`) > strings.Index(out, `"z\x1b" =`) {
		t.Fatal("unstable evidence ordering")
	}
}
