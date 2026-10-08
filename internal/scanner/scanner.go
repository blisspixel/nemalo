// Package scanner adapts installed antivirus tools without installing software or
// changing host policy. The caller supplies a private snapshot, never a source.
package scanner

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Status          string    `json:"status"`
	Tool            string    `json:"tool,omitempty"`
	StartedAt       time.Time `json:"started_at,omitzero"`
	ExitCode        int       `json:"exit_code"`
	Output          string    `json:"output,omitempty"`
	Error           string    `json:"error,omitempty"`
	OutputTruncated bool      `json:"output_truncated,omitempty"`
	Limitations     []string  `json:"limitations,omitempty"`
}

type Adapter struct {
	Resolve func() (string, string, error)
	Run     func(context.Context, string, []string) (string, int, bool, error)
}

func New() Adapter { return Adapter{Resolve: Resolve, Run: run} }

func Resolve() (string, string, error) {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("ProgramData"); base != "" {
			matches, _ := filepath.Glob(filepath.Join(base, "Microsoft", "Windows Defender", "Platform", "*", "MpCmdRun.exe"))
			sort.Slice(matches, func(i, j int) bool { return newerPlatform(matches[i], matches[j]) })
			for _, name := range matches {
				if info, err := os.Stat(name); err == nil && info.Mode().IsRegular() {
					return "defender", name, nil
				}
			}
		}
		if base := os.Getenv("ProgramFiles"); base != "" {
			name := filepath.Join(base, "Windows Defender", "MpCmdRun.exe")
			if info, err := os.Stat(name); err == nil && info.Mode().IsRegular() {
				return "defender", name, nil
			}
		}
	}
	name, err := exec.LookPath("clamscan")
	return "clamav", name, err
}

func newerPlatform(a, b string) bool {
	fields := func(p string) []string {
		return strings.FieldsFunc(filepath.Base(filepath.Dir(p)), func(r rune) bool { return r == '.' || r == '-' })
	}
	x, y := fields(a), fields(b)
	for i := 0; i < min(len(x), len(y)); i++ {
		u, ue := strconv.Atoi(x[i])
		v, ve := strconv.Atoi(y[i])
		if ue != nil || ve != nil {
			return a > b
		}
		if u != v {
			return u > v
		}
	}
	return len(x) > len(y)
}

func (a Adapter) Scan(ctx context.Context, file string) Result {
	r := Result{Status: "unavailable", ExitCode: -1, Limitations: []string{"No antivirus guarantees safety; signature freshness and internal scan coverage are not independently verified.", "External scanner cloud and sample-submission settings apply; Nemalo does not change them."}}
	kind, exe, err := a.Resolve()
	if err != nil {
		r.Output = err.Error()
		return r
	}
	r.Tool, r.StartedAt = exe, time.Now().UTC()
	args := []string{"--alert-exceeds-max=yes", "--alert-encrypted=yes", "--max-filesize=256M", "--max-scansize=512M", "--max-recursion=32", "--", file}
	if kind == "defender" {
		args = []string{"-Scan", "-ScanType", "3", "-File", file, "-DisableRemediation"}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	output, code, truncated, err := a.Run(ctx, exe, args)
	r.Output, r.ExitCode, r.Status = output, code, "incomplete"
	r.OutputTruncated = truncated
	if err != nil {
		r.Error = err.Error()
	}
	if ctx.Err() != nil {
		r.Error = ctx.Err().Error()
	}
	if ctx.Err() != nil || truncated {
		return r
	}
	if kind == "clamav" && code == 1 {
		r.Status = "findings"
		return r
	}
	if code != 0 || err != nil {
		return r
	}
	lower := strings.ToLower(output)
	if strings.Contains(lower, "error") || strings.Contains(lower, "warning") || strings.Contains(lower, "skipped") {
		return r
	}
	if (kind == "defender" && strings.Contains(lower, "found no threats")) || (kind == "clamav" && summaryLine(lower, "infected files: 0") && summaryLine(lower, "scanned files: 1")) {
		r.Status = "no_detections_reported"
	}
	return r
}

func summaryLine(output, wanted string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.Join(strings.Fields(line), " ") == wanted {
			return true
		}
	}
	return false
}

// A single capped writer receives merged stdout/stderr; exec serializes writes.
type capped struct {
	data      []byte
	truncated bool
}

func (w *capped) Write(p []byte) (int, error) {
	n := len(p)
	space := 64<<10 - len(w.data)
	if len(p) > space {
		w.truncated = true
		p = p[:space]
	}
	w.data = append(w.data, p...)
	return n, nil
}

func run(ctx context.Context, exe string, args []string) (string, int, bool, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.WaitDelay = 2 * time.Second
	out := &capped{}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	code := -1
	if cmd.ProcessState != nil {
		code = cmd.ProcessState.ExitCode()
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		out.truncated = true
	}
	return string(out.data), code, out.truncated, err
}
