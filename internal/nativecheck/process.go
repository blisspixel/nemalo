package nativecheck

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

const (
	maxStdout       = 1 << 20
	maxStderr       = 64 << 10
	commandDeadline = 30 * time.Second
)

type invocation struct {
	binary, directory string
	args, env         []string
	deadline          time.Duration // zero selects commandDeadline
}

type reply struct {
	stdout, stderr []byte
	exit           int
	err            error
}

type runner func(context.Context, invocation) reply

// Each stream has one exec copy goroutine. Cancellation stops a child as soon
// as either stream exceeds its allowance; overflow is never an expected refusal.
type boundedOutput struct {
	bytes    []byte
	limit    int
	cancel   context.CancelFunc
	exceeded bool
	mu       sync.Mutex
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := min(len(p), b.limit-len(b.bytes))
	b.bytes = append(b.bytes, p[:n]...)
	if n < len(p) {
		b.exceeded = true
		b.cancel()
	}
	return len(p), nil
}

func execute(ctx context.Context, in invocation) reply {
	deadline := commandDeadline
	if in.deadline > 0 {
		deadline = in.deadline
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	out := boundedOutput{limit: maxStdout, cancel: cancel}
	diagnostic := boundedOutput{limit: maxStderr, cancel: cancel}
	cmd := exec.CommandContext(ctx, in.binary, in.args...)
	cmd.Dir, cmd.Env = in.directory, in.env
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	r := reply{stdout: out.bytes, stderr: diagnostic.bytes, exit: -1}
	switch {
	case out.exceeded || diagnostic.exceeded:
		r.err = errors.New("child output exceeded its allowance")
	case ctx.Err() != nil:
		r.err = ctx.Err()
	case err == nil:
		r.exit = 0
	default:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			r.exit = exit.ExitCode()
		} else {
			r.err = err
		}
	}
	return r
}
