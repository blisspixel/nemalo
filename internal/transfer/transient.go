package transfer

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
)

// Transient reports a transfer that stopped for a transport reason: an
// unexpected end of body, a connection reset, a network error, an HTTP/2
// stream error, or a deadline. Such a transfer may be continued or tried
// again. Cancellation, size, checksum, budget, and write failures are not
// transient: trying again cannot change them.
func Transient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.As(err, &netErr) || strings.Contains(err.Error(), "stream error")
}
