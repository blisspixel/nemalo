//go:build !windows

package launch

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

// platformOpen runs xdg-open (freedesktop) or macOS open and waits for it to
// hand the file to the desktop. Their exit status reports a launch failure.
func platformOpen(ctx context.Context, file string) error {
	tool := "xdg-open"
	if runtime.GOOS == "darwin" {
		tool = "/usr/bin/open"
	}
	exe, err := exec.LookPath(tool)
	if err != nil {
		return fmt.Errorf("%s is not available; pass --app with a reader or player", tool)
	}
	out, err := exec.CommandContext(ctx, exe, file).CombinedOutput()
	if err != nil {
		if len(out) > 512 {
			out = out[:512]
		}
		return fmt.Errorf("%s failed: %w: %q", tool, err, out)
	}
	return nil
}
