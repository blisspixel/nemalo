package assessment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/blisspixel/nemalo/internal/decode"
)

// A decode result becomes evidence like a scan: errors, a requested decode
// that did not run, and a decoded duration far from the encoded one are
// findings; a clean, matching decode is not.
func TestDecodeResultsBecomeFindings(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "track.mp3"), mp3Frames(40), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, tc := range []struct {
		result  decode.Result
		finding string
	}{
		{decode.Result{Status: "passed", DecodedDurationMs: 1044}, ""},
		{decode.Result{Status: "passed", DecodedDurationMs: 3000}, "the decoded duration differs from the encoded duration by more than a second"},
		{decode.Result{Status: "errors_reported", DecodedDurationMs: 1044, Errors: 3}, "the audio decoder reported errors"},
		{decode.Result{Status: "unavailable"}, "an audio decode was requested but did not complete: unavailable"},
	} {
		p, err := prepare(ctx, root, "track.mp3", Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.bindDecode(ctx, tc.result); err != nil {
			t.Fatal(err)
		}
		r, _ := p.finish()
		_ = p.release()
		if r.Decode == nil || (tc.finding == "") != (len(r.Findings) == 0) || (tc.finding != "" && !slices.Contains(r.Findings, tc.finding)) {
			t.Errorf("%+v: findings %v", tc.result, r.Findings)
		}
		if tc.finding != "" && r.Status != "needs_review" {
			t.Errorf("%+v: status %s", tc.result, r.Status)
		}
	}
	// Without FFmpeg a requested decode is a finding through the whole check.
	saved := decode.LookPath
	decode.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	t.Cleanup(func() { decode.LookPath = saved })
	r, err := CheckInRoot(ctx, root, "track.mp3", Options{Decode: true}, nil)
	if err == nil || r.Decode == nil || r.Decode.Status != "unavailable" || r.Status != "needs_review" {
		t.Fatal(r.Decode, r.Status, err)
	}
	// Nothing is decoded unless asked.
	if r, _ := CheckInRoot(ctx, root, "track.mp3", Options{}, nil); r.Decode != nil {
		t.Fatal("a decode ran unrequested")
	}
}
