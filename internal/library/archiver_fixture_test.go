package library

import (
	"debug/buildinfo"
	"os"
	"runtime"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestArchiveFixtureRetainsToolchainAndInstrumentation(t *testing.T) {
	exe, err := stagingtest.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if testing.CoverMode() != "" {
		parent, err := os.Executable()
		if err != nil || exe != parent {
			t.Fatal("coverage must retain Go test's instrumented executable", exe, parent, err)
		}
		return
	}
	info, err := buildinfo.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if info.Path != "github.com/blisspixel/nemalo/internal/staging/stagingtest/cmd/archiver" || info.GoVersion != runtime.Version() {
		t.Fatal("fixture must use the small handler and the running toolchain", info)
	}
	race := false
	for _, setting := range info.Settings {
		if setting.Key == "-race" {
			race = setting.Value == "true"
		}
	}
	if race != libraryTestRace {
		t.Fatalf("fixture race instrumentation = %t; parent = %t", race, libraryTestRace)
	}
}
