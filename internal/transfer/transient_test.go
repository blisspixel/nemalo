package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
)

func TestTransientLooksBeneathTheRequestError(t *testing.T) {
	wrap := func(err error) error { return &url.Error{Op: "Get", URL: "https://archive.org/x", Err: err} }
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"early end of body":   {io.ErrUnexpectedEOF, true},
		"closed before reply": {wrap(io.EOF), true},
		"reset":               {wrap(&net.OpError{Op: "read", Err: os.NewSyscallError("read", syscall.ECONNRESET)}), true},
		"refused":             {wrap(&net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}), true},
		"timeout":             {wrap(os.ErrDeadlineExceeded), true},
		"temporary dns":       {wrap(&net.DNSError{Err: "server misbehaving", IsTemporary: true}), true},
		"no such host":        {wrap(&net.DNSError{Err: "no such host", IsNotFound: true}), false},
		"policy refusal":      {wrap(&net.OpError{Op: "dial", Err: errors.New("archive.org resolved to a non-public address")}), false},
		"plain request error": {wrap(errors.New("stop")), false},
		"checksum":            {errors.New("download differs from source checksum"), false},
		"cancelled":           {wrap(context.Canceled), false},
		"disk full":           {fmt.Errorf("write: %w", syscall.ENOSPC), false},
		"windows reset":       {wrap(errors.New("wsarecv: An existing connection was forcibly closed by the remote host.")), true},
		"http2 stream":        {errors.New("stream error: stream ID 3; INTERNAL_ERROR"), true},
		"nil":                 {nil, false},
	} {
		if got := Transient(tc.err); got != tc.want {
			t.Errorf("%s: Transient = %t, want %t", name, got, tc.want)
		}
	}
}
