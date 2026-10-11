//go:build windows

package launch

import (
	"context"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/windows"
)

// platformOpen calls ShellExecuteW with the default verb and no parameters, on
// a thread initialized for COM as the documentation requires. It avoids
// cmd.exe, whose parsing differs from argument quoting.
func platformOpen(_ context.Context, file string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const coinit = windows.COINIT_APARTMENTTHREADED | windows.COINIT_DISABLE_OLE1DDE
	if err := windows.CoInitializeEx(0, coinit); err == nil {
		defer windows.CoUninitialize()
	}
	path, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(file))
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, nil, path, nil, dir, windows.SW_SHOWNORMAL)
}
