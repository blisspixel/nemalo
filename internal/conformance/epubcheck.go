// Package conformance runs an installed EPUB conformance checker, EPUBCheck,
// on an assessed snapshot. It is an optional adapter: Nemalo works without it,
// never downloads it, and never treats a check that did not run as a pass.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Result is one EPUBCheck run. Status is "passed" (the report counts no
// fatal errors or errors; warnings may remain), "errors_reported",
// "unavailable" (not installed), or "incomplete" (it failed, timed out, or
// wrote no readable report). Only "passed" and "errors_reported" come from a
// report, and the counts are the report's own.
type Result struct {
	Tool            string    `json:"tool"`
	Status          string    `json:"status"`
	Executable      string    `json:"executable,omitempty"`
	Version         string    `json:"version,omitempty"`
	Fatal           int       `json:"fatal"`
	Errors          int       `json:"errors"`
	Warnings        int       `json:"warnings"`
	Usage           int       `json:"usage"`
	Messages        []Message `json:"messages"`
	MessagesOmitted int       `json:"messages_omitted,omitempty"`
	Detail          string    `json:"detail,omitempty"`
}

// Message is one reported message. Locations are paths inside the EPUB.
type Message struct {
	ID        string     `json:"id"`
	Severity  string     `json:"severity"`
	Text      string     `json:"text"`
	Locations []Location `json:"locations,omitempty"`
}

type Location struct {
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

const (
	maxReport    = 16 << 20
	maxMessages  = 200
	maxLocations = 5
	deadline     = 5 * time.Minute
)

// LookPath finds the checker; tests replace it.
var LookPath = func() (string, error) { return exec.LookPath("epubcheck") }

// CheckEPUB runs the installed EPUBCheck on file and reads its JSON report
// from a private directory. The file is passed as an argument, never through
// a shell, and nothing in it is executed by Nemalo.
func CheckEPUB(ctx context.Context, file string) Result {
	r := Result{Tool: "epubcheck", Status: "incomplete", Messages: []Message{}}
	exe, err := LookPath()
	if err != nil {
		r.Status, r.Detail = "unavailable", "epubcheck is not on PATH"
		return r
	}
	r.Executable = exe
	dir, err := os.MkdirTemp("", "nemalo-epubcheck-")
	if err != nil {
		r.Detail = textField(err.Error(), 300)
		return r
	}
	defer os.RemoveAll(dir)
	report := filepath.Join(dir, "report.json")
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, file, "--json", report)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	runErr := cmd.Run()
	if ctx.Err() != nil {
		r.Detail = "epubcheck did not finish within its deadline"
		return r
	}
	data, err := readBounded(report)
	if err != nil {
		r.Detail = "epubcheck wrote no readable report"
		if runErr != nil {
			r.Detail += ": " + textField(runErr.Error(), 200)
		}
		return r
	}
	if err := parseReport(data, &r); err != nil {
		r.Detail = "epubcheck report was not understood: " + textField(err.Error(), 200)
		return r
	}
	return r
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxReport+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReport {
		return nil, errors.New("report exceeds 16 MiB")
	}
	return data, nil
}

// parseReport reads EPUBCheck's report. The checker block must name a
// version and give non-negative counts that agree with the messages listed.
func parseReport(data []byte, r *Result) error {
	var wire struct {
		Checker *struct {
			Version *string `json:"checkerVersion"`
			Fatal   *int    `json:"nFatal"`
			Error   *int    `json:"nError"`
			Warning *int    `json:"nWarning"`
			Usage   *int    `json:"nUsage"`
		} `json:"checker"`
		Messages *[]struct {
			ID        string `json:"ID"`
			Severity  string `json:"severity"`
			Message   string `json:"message"`
			Locations []struct {
				Path   string `json:"path"`
				Line   int    `json:"line"`
				Column int    `json:"column"`
			} `json:"locations"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	c := wire.Checker
	if c == nil || c.Version == nil || c.Fatal == nil || c.Error == nil || c.Warning == nil || c.Usage == nil || wire.Messages == nil {
		return errors.New("checker version, counts, or messages missing")
	}
	if *c.Fatal < 0 || *c.Error < 0 || *c.Warning < 0 || *c.Usage < 0 {
		return errors.New("negative counts")
	}
	r.Version = textField(*c.Version, 100)
	r.Fatal, r.Errors, r.Warnings, r.Usage = *c.Fatal, *c.Error, *c.Warning, *c.Usage
	listed := map[string]int{}
	for _, m := range *wire.Messages {
		severity := strings.ToUpper(m.Severity)
		switch severity {
		case "FATAL", "ERROR", "WARNING", "USAGE", "INFO":
		default:
			return fmt.Errorf("unknown severity %q", textField(m.Severity, 20))
		}
		listed[severity]++
		if len(r.Messages) >= maxMessages {
			r.MessagesOmitted++
			continue
		}
		msg := Message{ID: textField(m.ID, 50), Severity: severity, Text: textField(m.Message, 1000)}
		for i, l := range m.Locations {
			if i >= maxLocations {
				break
			}
			msg.Locations = append(msg.Locations, Location{Path: textField(l.Path, 500), Line: max(l.Line, 0), Column: max(l.Column, 0)})
		}
		r.Messages = append(r.Messages, msg)
	}
	// A report that counts errors it does not list, or lists errors it does
	// not count, is not taken at its word.
	if (listed["FATAL"] > 0) != (r.Fatal > 0) || (listed["ERROR"] > 0) != (r.Errors > 0) {
		return errors.New("counts disagree with the listed messages")
	}
	r.Status = "passed"
	if r.Fatal > 0 || r.Errors > 0 {
		r.Status = "errors_reported"
	}
	return nil
}

// textField keeps reported text printable, single-line, and bounded.
func textField(s string, limit int) string {
	s = strings.Join(strings.FieldsFunc(strings.ToValidUTF8(s, ""), func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
	if len(s) > limit {
		end := limit
		for end > 0 && !utf8.RuneStart(s[end]) {
			end--
		}
		s = s[:end]
	}
	return strings.TrimSpace(s)
}
