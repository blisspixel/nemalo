package transfer

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
)

// Transient reports a transfer that stopped for a transport reason that
// another try may not meet: a body that ended early, a connection reset,
// refused, aborted, or unreachable at the socket, a temporary DNS failure, a
// timeout, or an HTTP/2 stream error. Every request error is wrapped in a
// *url.Error, which is a net.Error, so the cause underneath is what counts:
// a refused destination, a policy check, a size or checksum mismatch, a
// budget, a write failure, or cancellation is not transient.
func Transient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	for _, errno := range []syscall.Errno{syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ECONNABORTED, syscall.EPIPE, syscall.ETIMEDOUT, syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ENETDOWN} {
		if errors.Is(err, errno) {
			return true
		}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return dns.IsTimeout || dns.IsTemporary
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return true
	}
	// Windows reports resets with its own error numbers, and HTTP/2 names
	// stream failures only in text.
	msg := err.Error()
	return strings.Contains(msg, "stream error") || strings.Contains(msg, "connection reset") || strings.Contains(msg, "forcibly closed")
}
