package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScannerStatesAndArguments(t *testing.T) {
	for _, tc := range []struct {
		kind, output string
		code         int
		truncated    bool
		err          error
		want         string
	}{
		{"clamav", "Scanned files: 1\nInfected files: 0", 0, false, nil, "no_detections_reported"},
		{"clamav", "Scanned files: 10\nInfected files: 0", 0, false, nil, "incomplete"},
		{"clamav", "Scanned files: 1\nInfected files: 0\nWARNING skipped", 0, false, nil, "incomplete"},
		{"clamav", "Heuristics.Limits.Exceeded FOUND", 1, false, errors.New("exit 1"), "findings"},
		{"clamav", "error", 2, false, errors.New("exit 2"), "incomplete"},
		{"defender", "Scanning file found no threats.", 0, false, nil, "no_detections_reported"},
		{"defender", "Scan finished", 0, false, nil, "incomplete"},
		{"defender", "found no threats", 0, true, nil, "incomplete"},
		{"defender", "found no threats", 0, false, errors.New("wait failed"), "incomplete"},
	} {
		t.Run(tc.kind+tc.want+tc.output, func(t *testing.T) {
			a := Adapter{Resolve: func() (string, string, error) { return tc.kind, "scanner", nil }, Run: func(_ context.Context, exe string, args []string) (string, int, bool, error) {
				joined := strings.Join(args, " ")
				if exe != "scanner" || !strings.Contains(joined, "private-file") {
					t.Fatal("bad invocation")
				}
				if tc.kind == "defender" && !strings.Contains(joined, "-DisableRemediation") {
					t.Fatal("remediation enabled")
				}
				if tc.kind == "clamav" && (!strings.Contains(joined, "--alert-exceeds-max=yes") || !strings.Contains(joined, "--alert-encrypted=yes") || strings.Contains(joined, "--remove")) {
					t.Fatal("unsafe scan arguments")
				}
				return tc.output, tc.code, tc.truncated, tc.err
			}}
			if r := a.Scan(context.Background(), "private-file"); r.Status != tc.want {
				t.Fatal(r)
			}
		})
	}
	a := Adapter{Resolve: func() (string, string, error) { return "", "", errors.New("missing") }}
	if a.Scan(context.Background(), "file").Status != "unavailable" {
		t.Fatal("missing scanner hidden")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a = Adapter{Resolve: func() (string, string, error) { return "clamav", "tool", nil }, Run: func(context.Context, string, []string) (string, int, bool, error) {
		return "Scanned files: 1\nInfected files: 0", 0, false, nil
	}}
	if a.Scan(ctx, "file").Status != "incomplete" {
		t.Fatal("cancelled scan passed")
	}
	if New().Resolve == nil || New().Run == nil {
		t.Fatal("missing default boundaries")
	}
}

func TestProcessBoundary(t *testing.T) {
	if os.Getenv("NEMALO_SCANNER_TEST") == "1" {
		fmt.Print(strings.Repeat("x", 70<<10))
		os.Exit(0)
	}
	t.Setenv("NEMALO_SCANNER_TEST", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output, code, truncated, err := run(context.Background(), exe, []string{"-test.run=^TestProcessBoundary$"})
	if err != nil || code != 0 || !truncated || len(output) != 64<<10 {
		t.Fatal(len(output), code, truncated, err)
	}
	if _, code, _, err := run(context.Background(), "nonexistent-nemalo-scanner", nil); err == nil || code != -1 {
		t.Fatal(code, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := run(ctx, exe, nil); err == nil {
		t.Fatal("cancelled command executed")
	}
}

func TestResolveInstalledOrUnavailable(t *testing.T) {
	kind, path, err := Resolve()
	if err == nil && (path == "" || (kind != "clamav" && kind != "defender")) {
		t.Fatal(kind, path)
	}
}

func TestPlatformVersionOrdering(t *testing.T) {
	p := func(version string) string { return filepath.Join("platform", version, "MpCmdRun.exe") }
	for _, tc := range []struct {
		a, b  string
		newer bool
	}{{"4.18.26080.10-0", "4.18.26080.9-0", true}, {"4.18.26090.1-0", "4.18.26080.10-0", true}, {"4.18.26080.9-0", "4.18.26080.10-0", false}, {"4.18.1", "4.18", true}, {"4.18", "4.18", false}, {"bad", "alpha", true}} {
		if got := newerPlatform(p(tc.a), p(tc.b)); got != tc.newer {
			t.Fatal(tc, got)
		}
	}
}
