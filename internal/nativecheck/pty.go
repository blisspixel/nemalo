package nativecheck

import "io"

type terminalExit struct {
	code int
	err  error
}

type terminalProcess interface {
	io.ReadWriteCloser
	Resize(int, int) error
	Wait() terminalExit
	Kill() error
}
