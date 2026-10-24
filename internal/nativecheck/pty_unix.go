//go:build linux || darwin

package nativecheck

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

type unixTerminal struct {
	*os.File
	cmd *exec.Cmd
}

func startTerminal(in invocation, width, height int) (terminalProcess, error) {
	cmd := exec.Command(in.binary, in.args...)
	cmd.Dir, cmd.Env = in.directory, in.env
	file, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		return nil, err
	}
	return &unixTerminal{file, cmd}, nil
}

func (p *unixTerminal) Resize(w, h int) error {
	return pty.Setsize(p.File, &pty.Winsize{Cols: uint16(w), Rows: uint16(h)})
}
func (p *unixTerminal) Wait() terminalExit {
	err := p.cmd.Wait()
	if p.cmd.ProcessState == nil {
		return terminalExit{-1, err}
	}
	return terminalExit{p.cmd.ProcessState.ExitCode(), err}
}
func (p *unixTerminal) Kill() error { return p.cmd.Process.Kill() }
func terminalEOF(err error) bool    { return errors.Is(err, io.EOF) || errors.Is(err, syscall.EIO) }
