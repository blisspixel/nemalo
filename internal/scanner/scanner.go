// Package scanner adapts installed antivirus tools without installing software or
// changing host policy. The caller supplies a private snapshot, never a source.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var scanLimitations = []string{"No antivirus guarantees safety; signature freshness and internal scan coverage are not independently verified.", "External scanner cloud and sample-submission settings apply; Nemalo does not change them."}

// clamArgs keep limits, encryption, and exceeded-size cases visible as alerts.
// Without them ClamAV may stop scanning part of a file and still report OK.
var clamArgs = []string{"--alert-exceeds-max=yes", "--alert-encrypted=yes", "--max-filesize=256M", "--max-scansize=512M", "--max-recursion=32", "--"}

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
	r := Result{Status: "unavailable", ExitCode: -1, Limitations: append([]string{}, scanLimitations...)}
	kind, exe, err := a.Resolve()
	if err != nil {
		r.Output = err.Error()
		return r
	}
	r.Tool, r.StartedAt = exe, time.Now().UTC()
	args := append(append([]string{}, clamArgs...), file)
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
	if kind == "defender" && code == 2 && defenderDetection(output) {
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

// Defender exits 2 both for unremediated detections and for scan errors, so
// the exit code alone is not a detection. A detection needs the scan summary
// naming a positive threat count and a threat entry; errors print "[Failed]".
// Unrecognized output stays incomplete rather than becoming clean or detected.
var (
	defenderThreatSummary = regexp.MustCompile(`(?m)^Scanning .+ found [1-9][0-9]* threats?\.\s*$`)
	defenderThreatEntry   = regexp.MustCompile(`(?m)^\s*Threat\s*:\s*\S`)
)

func defenderDetection(output string) bool {
	return !strings.Contains(output, "[Failed]") && defenderThreatSummary.MatchString(output) && defenderThreatEntry.MatchString(output)
}

// Kind reports which scanner the adapter would use, without running it.
func (a Adapter) Kind() (string, error) {
	kind, _, err := a.Resolve()
	return kind, err
}

// ScanMany scans several private snapshots. ClamAV receives them in one
// invocation so its signature database loads once; that load dominates the
// cost of scanning a small file. Defender accepts one -File per invocation and
// loads no database per process, so it scans each file in turn. Results align
// with files by index.
func (a Adapter) ScanMany(ctx context.Context, files []string) []Result {
	results := make([]Result, len(files))
	kind, exe, err := a.Resolve()
	if err != nil || kind != "clamav" || len(files) < 2 {
		for i, file := range files {
			results[i] = a.Scan(ctx, file)
		}
		return results
	}
	started := time.Now().UTC()
	// One database load plus bounded per-file work; the cap keeps a batch finite.
	ctx, cancel := context.WithTimeout(ctx, min(2*time.Minute+time.Duration(len(files))*30*time.Second, 20*time.Minute))
	defer cancel()
	output, code, truncated, runErr := a.Run(ctx, exe, append(append([]string{}, clamArgs...), files...))
	statuses := clamBatchStatuses(output, files, code)
	for i := range results {
		r := Result{Status: statuses[i], Tool: exe, StartedAt: started, ExitCode: code, OutputTruncated: truncated, Output: clamFileLines(output, files[i]), Limitations: append([]string{}, scanLimitations...)}
		if runErr != nil {
			r.Error = runErr.Error()
		}
		if ctx.Err() != nil {
			r.Error = ctx.Err().Error()
		}
		// A detection line is a positive report even if the batch then failed;
		// every other status needs a complete, untruncated, successful run.
		if r.Status != "findings" && (ctx.Err() != nil || truncated || (runErr != nil && code != 1)) {
			r.Status = "incomplete"
		}
		results[i] = r
	}
	return results
}

// clamBatchStatuses attributes clamscan's per-file lines. A file is clean only
// when its single line is "path: OK", no unattributed warning or error exists,
// the summary accounts for every file, and the exit code agrees. Anything else
// is incomplete. A "FOUND" line is a detection for that file.
func clamBatchStatuses(output string, files []string, code int) []string {
	statuses := make([]string, len(files))
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	found := 0
	// A missing or repeated per-file line means the output cannot be fully
	// attributed, so no file in this batch is reported clean.
	inconsistent := false
	for i, file := range files {
		matches := 0
		for _, line := range lines {
			rest, ok := strings.CutPrefix(line, file+": ")
			if !ok {
				continue
			}
			matches++
			switch {
			case strings.HasSuffix(rest, " FOUND"):
				statuses[i] = "findings"
			case rest == "OK" && statuses[i] == "":
				statuses[i] = "no_detections_reported"
			case statuses[i] != "findings":
				statuses[i] = "incomplete"
			}
		}
		if matches != 1 && statuses[i] != "findings" {
			statuses[i] = "incomplete"
			inconsistent = true
		}
		if statuses[i] == "findings" {
			found++
		}
	}
	unattributed := false
	for _, line := range lines {
		owned := false
		for _, file := range files {
			owned = owned || strings.HasPrefix(line, file+": ")
		}
		lower := strings.ToLower(line)
		if !owned && (strings.Contains(lower, "error") || strings.Contains(lower, "warning") || strings.Contains(lower, "skipped")) {
			unattributed = true
		}
	}
	lower := strings.ToLower(output)
	summarized := summaryLine(lower, fmt.Sprintf("scanned files: %d", len(files))) && summaryLine(lower, fmt.Sprintf("infected files: %d", found))
	expected := 0
	if found > 0 {
		expected = 1
	}
	for i := range statuses {
		if statuses[i] == "no_detections_reported" && (inconsistent || unattributed || !summarized || code != expected) {
			statuses[i] = "incomplete"
		}
	}
	return statuses
}

// clamFileLines keeps only the batch output lines that name this file.
func clamFileLines(output, file string) string {
	var kept []string
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, file+": ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
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
