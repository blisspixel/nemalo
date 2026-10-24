//go:build !linux && !darwin && !windows

package nativecheck

import "errors"

func startTerminal(invocation, int, int) (terminalProcess, error) {
	return nil, errors.New("native terminal acceptance supports Linux, macOS and Windows only")
}
func terminalEOF(error) bool { return false }
