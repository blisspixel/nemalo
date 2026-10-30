// Package tempdirtest gives a test binary its own temporary folder. Production
// code leaves acquisition output locks in the system temporary folder by
// design (decision 0108); a test binary removes its folder when it ends.
package tempdirtest

import (
	"fmt"
	"os"
	"testing"
)

// Run points TMPDIR, TMP and TEMP at a new folder, runs the tests and removes
// the folder. Call it from TestMain as os.Exit(tempdirtest.Run(m)). A folder
// that cannot be removed fails the run, since a test left something open.
func Run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "nemalo-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "create the test temporary folder:", err)
		return 1
	}
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		if err := os.Setenv(name, dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			_ = os.RemoveAll(dir)
			return 1
		}
	}
	code := m.Run()
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, "remove the test temporary folder:", err)
		return 1
	}
	return code
}
