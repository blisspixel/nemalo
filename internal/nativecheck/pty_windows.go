//go:build windows

package nativecheck

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/charmbracelet/x/conpty"
	"golang.org/x/sys/windows"
)

// drainBeforeClose is false: ConPTY output ends only when the pseudoconsole
// is closed, so the worker is joined after Close.
const drainBeforeClose = false

type windowsTerminal struct {
	*conpty.ConPty
	process *os.Process
}

func startTerminal(in invocation, width, height int) (terminalProcess, error) {
	p, err := conpty.New(width, height, 0)
	if err != nil {
		return nil, err
	}
	pid, handle, err := p.Spawn(in.binary, append([]string{in.binary}, in.args...), &syscall.ProcAttr{Dir: in.directory, Env: in.env})
	if err != nil {
		return nil, errors.Join(err, p.Close())
	}
	process, findErr := os.FindProcess(pid)
	if findErr != nil {
		killErr := windows.TerminateProcess(windows.Handle(handle), 1)
		return nil, errors.Join(findErr, killErr, windows.CloseHandle(windows.Handle(handle)), p.Close())
	}
	if err := windows.CloseHandle(windows.Handle(handle)); err != nil {
		killErr := process.Kill()
		_, waitErr := process.Wait()
		return nil, errors.Join(err, killErr, waitErr, p.Close())
	}
	return &windowsTerminal{p, process}, nil
}

func (p *windowsTerminal) Wait() terminalExit {
	state, err := p.process.Wait()
	if err != nil {
		return terminalExit{-1, err}
	}
	if !state.Success() {
		err = &exec.ExitError{ProcessState: state}
	}
	return terminalExit{state.ExitCode(), err}
}
func (p *windowsTerminal) Kill() error { return p.process.Kill() }
func terminalEOF(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_HANDLE_EOF)
}
