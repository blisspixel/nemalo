// Package launch hands a file to a desktop application. It never uses a
// shell: Linux runs xdg-open and macOS runs open with an argument array, and
// Windows calls ShellExecuteW directly. A successful launch means the
// application was started, not that it opened or understood the file.
package launch

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// With starts an explicitly chosen application with the file as its only
// argument. The application path is resolved like a command, never parsed.
func With(ctx context.Context, app, file string) error {
	if !filepath.IsAbs(file) {
		return errors.New("launch requires an absolute file path")
	}
	exe, err := exec.LookPath(app)
	if err != nil {
		return fmt.Errorf("application %q is not available: %w", app, err)
	}
	cmd := exec.Command(exe, file)
	// The application keeps running; it is not waited for or killed on return.
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Default opens the file with the desktop's default application.
func Default(ctx context.Context, file string) error {
	if !filepath.IsAbs(file) {
		return errors.New("launch requires an absolute file path")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return platformOpen(ctx, file)
}
