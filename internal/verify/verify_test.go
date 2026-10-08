package verify

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoverageThreshold(t *testing.T) {
	for _, tc := range []struct {
		profile string
		pass    bool
	}{{"mode: atomic\nx.go:1.1,2.1 80 1\nx.go:2.1,3.1 20 0\n", true}, {"mode: atomic\nx.go:1.1,2.1 79 1\nx.go:2.1,3.1 21 0\n", false}, {"mode: count\nx 1 1\n", false}, {"mode: atomic\n", false}, {"mode: atomic\nwrong\n", false}, {"mode: atomic\nx bad 1\n", false}, {"mode: atomic\nx 1 bad\n", false}} {
		if err := CheckCoverage(strings.NewReader(tc.profile)); (err == nil) != tc.pass {
			t.Fatalf("profile %q: %v", tc.profile, err)
		}
	}
}

func TestVerificationOrderAndFailures(t *testing.T) {
	file := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(file, []byte("mode: atomic\nx.go:1.1,2.1 8 1\nx.go:2.1,3.1 2 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	open := func(string) (*os.File, error) { return os.Open(file) }
	var commands []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	var out bytes.Buffer
	if err := Steps(context.Background(), &out, run, open); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 6 || !strings.Contains(commands[3], "staticcheck") || !strings.Contains(commands[4], "-coverpkg=./...") || !strings.Contains(commands[5], "govulncheck") {
		t.Fatal(commands)
	}
	if err := Steps(context.Background(), io.Discard, func(context.Context, string, ...string) ([]byte, error) { return []byte("x.go\n"), nil }, open); err == nil {
		t.Fatal("formatting differences ignored")
	}
	if err := Steps(context.Background(), io.Discard, func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("failed") }, open); err == nil {
		t.Fatal("failed command ignored")
	}
	if err := Steps(context.Background(), io.Discard, run, func(string) (*os.File, error) { return nil, errors.New("missing profile") }); err == nil {
		t.Fatal("missing coverage ignored")
	}
	if err := os.WriteFile(file, []byte("mode: atomic\nx 1 0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Steps(context.Background(), io.Discard, run, open); err == nil {
		t.Fatal("low coverage ignored")
	}
}

func TestCoverageMergesCrossPackageBlocks(t *testing.T) {
	profile := "mode: atomic\na.go:1.1,2.1 8 1\na.go:2.1,3.1 2 0\na.go:1.1,2.1 8 0\na.go:2.1,3.1 2 0\n"
	got, err := Coverage(strings.NewReader(profile))
	if err != nil || got != 80 {
		t.Fatalf("duplicate blocks changed coverage: %.2f %v", got, err)
	}
	if _, err := Coverage(strings.NewReader("mode: atomic\na.go:1.1,2.1 8 1\na.go:1.1,2.1 7 1\n")); err == nil {
		t.Fatal("inconsistent duplicate accepted")
	}
}
