package present

import (
	"errors"
	"io"
	"io/fs"
	"net/url"
	"strconv"
	"strings"
	"syscall"

	"github.com/blisspixel/nemalo/internal/textsafe"
)

// Error states a failure as one plain line for a terminal. A request error
// names its host instead of the whole URL, a file error says what is wrong
// with the path, and source text in the message can neither move the cursor
// nor start another line. --json keeps the full message.
func Error(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	var ue *url.Error
	if errors.As(err, &ue) {
		host := "the source"
		if u, parseErr := url.Parse(ue.URL); parseErr == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
		cause := Error(ue.Err)
		if ue.Timeout() {
			cause = "no response in time"
		}
		msg = strings.Replace(msg, ue.Error(), "request to "+host+" failed: "+cause, 1)
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		msg = strings.Replace(msg, pe.Error(), pathProblem(pe), 1)
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		msg = strings.Replace(msg, io.ErrUnexpectedEOF.Error(), "the connection closed before the whole response arrived", 1)
	}
	return Line(msg)
}

func pathProblem(pe *fs.PathError) string {
	path := pe.Path
	switch {
	case errors.Is(pe.Err, fs.ErrNotExist):
		return path + " does not exist"
	case errors.Is(pe.Err, fs.ErrExist):
		return path + " already exists"
	case errors.Is(pe.Err, fs.ErrPermission):
		return "permission denied for " + path
	case errors.Is(pe.Err, syscall.ENOTDIR):
		return "a part of " + path + " is not a folder"
	case errors.Is(pe.Err, syscall.ENOSPC):
		return "no space left on the device holding " + path
	}
	return path + ": " + pe.Err.Error()
}

// Line makes untrusted text safe on one terminal row: control and format
// characters become visible escapes, and line breaks, tabs, and runs of
// spaces become single spaces, so the text cannot pose as other output.
func Line(s string) string {
	return strings.Join(strings.Fields(textsafe.Visible(s)), " ")
}

// Exact shows a path or identifier so its exact value is clear: as it is when
// it is plain one-row text, otherwise Go-quoted, so leading, trailing, or
// repeated spaces and any control character stay visible.
func Exact(s string) string {
	if s == "" || (Line(s) == s && strings.TrimSpace(s) == s) {
		return s
	}
	return strconv.Quote(s)
}
