package present

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestErrorIsOnePlainSentence(t *testing.T) {
	refused := &url.Error{Op: "Get", URL: "https://archive.org/advancedsearch.php?fl%5B%5D=identifier&q=%28frankenstein%29&rows=10", Err: errors.New("archive.org resolved to a non-public address")}
	_, missing := os.Lstat(filepath.Join(t.TempDir(), "lib"))
	for _, tc := range []struct {
		err  error
		want string
	}{
		{refused, "request to archive.org failed: archive.org resolved to a non-public address"},
		{fmt.Errorf("search: %w", refused), "search: request to archive.org failed: archive.org resolved to a non-public address"},
		{&url.Error{Op: "Get", URL: "https://openlibrary.org/x", Err: timeoutErr{}}, "request to openlibrary.org failed: no response in time"},
		{&url.Error{Op: "Get", URL: "::", Err: errors.New("boom")}, "request to the source failed: boom"},
		{fmt.Errorf("catalog stopped: %w", io.ErrUnexpectedEOF), "catalog stopped: the connection closed before the whole response arrived"},
		{&fs.PathError{Op: "open", Path: "/srv/x", Err: fs.ErrPermission}, "permission denied for /srv/x"},
		{&fs.PathError{Op: "open", Path: "/srv/x", Err: fs.ErrExist}, "/srv/x already exists"},
		{&fs.PathError{Op: "open", Path: "/srv/x", Err: errors.New("odd")}, "/srv/x: odd"},
		// Source text cannot move the cursor or pose as another output line.
		{errors.New("bad title\n\x1b[2Jnemalo: checked\tok"), `bad title \u001b[2Jnemalo: checked ok`},
		{context.Canceled, "context canceled"},
	} {
		if got := Error(tc.err); got != tc.want {
			t.Fatalf("%v\n got %q\nwant %q", tc.err, got, tc.want)
		}
	}
	if got := Error(missing); !strings.HasSuffix(got, string(filepath.Separator)+"lib does not exist") || strings.Contains(got, "lstat") {
		t.Fatal(got)
	}
	if Error(nil) != "" {
		t.Fatal("nil")
	}
}
