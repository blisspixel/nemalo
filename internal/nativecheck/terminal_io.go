package nativecheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/vt"
)

const (
	terminalOutputLimit = 2 << 20
	terminalInputLimit  = 64 << 10
	terminalFrameLimit  = 512 << 10
	terminalDeadline    = time.Minute
	terminalTeardown    = 5 * time.Second
)

type terminalStarter func(invocation, int, int) (terminalProcess, error)
type terminalInput struct {
	data []byte
	ack  chan error
}
type terminalScreen struct {
	text          string
	width, height int
	sequence      uint64
	alt           bool
}

// Only the output worker and callers holding mu may touch the emulator. Its
// input reader uses the pipe only; close that pipe and join it before Close
// changes the emulator's closed flag.
type terminalSession struct {
	ctx                                                    context.Context
	cancel                                                 context.CancelFunc
	process                                                terminalProcess
	mu                                                     sync.Mutex
	emulator                                               *vt.Emulator
	replyClose                                             io.Closer
	raw                                                    []byte
	rawRead                                                int
	limit                                                  int
	sequence                                               uint64
	frozen                                                 bool
	errMu                                                  sync.Mutex
	err                                                    error
	inputBytes                                             atomic.Int64
	stopping                                               atomic.Bool
	input                                                  chan terminalInput
	changed                                                chan struct{}
	exit                                                   terminalExit
	exitReady, outputDone, inputDone, replyDone, watchDone chan struct{}
}

func terminalEnvironment(root string) []string {
	env := childEnvironment(root)
	env = slices.DeleteFunc(env, func(value string) bool {
		key, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(key) {
		case "TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "COLORTERM", "NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE", "FORCE_COLOR", "TMUX", "TMUX_PANE", "STY", "VTE_VERSION", "WT_SESSION", "WT_PROFILE_ID", "ITERM_SESSION_ID", "KITTY_WINDOW_ID", "WEZTERM_EXECUTABLE":
			return true
		}
		return strings.HasPrefix(value, "=") // No drive-relative paths are used.
	})
	env = append(env, "TERM=xterm-256color", "TERM_PROGRAM=nemalo-native-check", "NO_COLOR=1", "CLICOLOR=0", "CLICOLOR_FORCE=0", "FORCE_COLOR=0")
	// The maintained ConPTY spawn API consumes a Windows environment block.
	slices.SortFunc(env, func(a, b string) int {
		a, _, _ = strings.Cut(a, "=")
		b, _, _ = strings.Cut(b, "=")
		return strings.Compare(strings.ToUpper(a), strings.ToUpper(b))
	})
	return env
}

func validTerminalGeometry(w, h int) bool { return w >= 24 && w <= 120 && h >= 12 && h <= 40 }

func newTerminalSession(parent context.Context, in invocation, limit int, start terminalStarter) (*terminalSession, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > terminalOutputLimit {
		return nil, errors.New("invalid terminal output allowance")
	}
	ctx, cancel := context.WithTimeout(parent, terminalDeadline)
	e := vt.NewEmulator(116, 36)
	e.SetScrollbackSize(1)
	closer, ok := e.InputPipe().(io.Closer)
	if !ok {
		cancel()
		_ = e.Close()
		return nil, errors.New("terminal reply pipe cannot be closed")
	}
	p, err := start(in, 116, 36)
	if err != nil {
		cancel()
		_ = e.Close()
		return nil, err
	}
	s := &terminalSession{ctx: ctx, cancel: cancel, process: p, emulator: e, replyClose: closer, limit: limit, input: make(chan terminalInput, 16), changed: make(chan struct{}, 1), exitReady: make(chan struct{}), outputDone: make(chan struct{}), inputDone: make(chan struct{}), replyDone: make(chan struct{}), watchDone: make(chan struct{})}
	go func() { s.exit = p.Wait(); close(s.exitReady); s.notify() }()
	go func() {
		defer close(s.watchDone)
		select {
		case <-ctx.Done():
			_ = p.Kill()
		case <-s.exitReady:
		}
	}()
	go s.readOutput()
	go s.writeInput()
	go s.readReplies()
	return s, nil
}

func (s *terminalSession) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}
func (s *terminalSession) fail(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.errMu.Unlock()
	s.cancel()
	s.notify()
}
func (s *terminalSession) failure() error { s.errMu.Lock(); defer s.errMu.Unlock(); return s.err }

func (s *terminalSession) readOutput() {
	defer close(s.outputDone)
	buf := make([]byte, 4096)
	for {
		s.mu.Lock()
		left := s.limit - s.rawRead
		s.mu.Unlock()
		if left < 0 {
			return
		}
		n, err := s.process.Read(buf[:min(len(buf), left+1)])
		if n > 0 {
			s.mu.Lock()
			s.rawRead += n
			take := min(n, s.limit-len(s.raw))
			s.raw = append(s.raw, buf[:take]...)
			over := s.rawRead > s.limit
			if !s.frozen && !over {
				_, writeErr := s.emulator.Write(buf[:n])
				if writeErr != nil {
					s.fail(writeErr)
				}
				s.emulator.ClearScrollback()
			}
			s.sequence++
			s.mu.Unlock()
			s.notify()
			if over {
				s.fail(errors.New("terminal output exceeded its allowance"))
				return
			}
		}
		if err != nil {
			if !terminalEOF(err) && !s.stopping.Load() {
				s.fail(err)
			}
			return
		}
		if n == 0 {
			s.fail(io.ErrNoProgress)
			return
		}
	}
}

func (s *terminalSession) reserveInput(n int) error {
	if n > 4096 || s.inputBytes.Add(int64(n)) > terminalInputLimit {
		err := errors.New("terminal input exceeded its allowance")
		s.fail(err)
		return err
	}
	return nil
}
func (s *terminalSession) readReplies() {
	defer close(s.replyDone)
	defer s.replyClose.Close()
	buf := make([]byte, 4096)
	for {
		n, err := s.emulator.Read(buf)
		if n > 0 {
			if s.reserveInput(n) != nil {
				return
			}
			r := terminalInput{data: append([]byte{}, buf[:n]...)}
			select {
			case s.input <- r:
			case <-s.ctx.Done():
				return
			default:
				s.fail(errors.New("terminal reply queue exceeded its allowance"))
				return
			}
		}
		if err != nil {
			return
		}
	}
}
func (s *terminalSession) writeInput() {
	defer close(s.inputDone)
	for {
		select {
		case <-s.ctx.Done():
			return
		case r := <-s.input:
			n, err := s.process.Write(r.data)
			if err == nil && n != len(r.data) {
				err = io.ErrShortWrite
			}
			if r.ack != nil {
				r.ack <- err
			}
			if err != nil {
				s.fail(err)
				return
			}
		}
	}
}

func (s *terminalSession) screen() terminalScreen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return terminalScreen{s.emulator.String(), s.emulator.Width(), s.emulator.Height(), s.sequence, s.emulator.IsAltScreen()}
}
func (s *terminalSession) alive() error {
	if err := s.failure(); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	select {
	case <-s.exitReady:
		return fmt.Errorf("terminal exited before the expected screen: exit %d, %v", s.exit.code, s.exit.err)
	default:
	}
	select {
	case <-s.outputDone:
		return errors.New("terminal output closed before the expected screen")
	default:
	}
	return nil
}
func (s *terminalSession) send(text string) (uint64, error) {
	sequence := s.screen().sequence
	if err := s.alive(); err != nil {
		return sequence, err
	}
	if err := s.reserveInput(len(text)); err != nil {
		return sequence, err
	}
	r := terminalInput{data: []byte(text), ack: make(chan error, 1)}
	select {
	case s.input <- r:
	case <-s.ctx.Done():
		return sequence, s.ctx.Err()
	}
	select {
	case err := <-r.ack:
		return sequence, err
	case <-s.ctx.Done():
		return sequence, s.ctx.Err()
	}
}
func (s *terminalSession) resize(w, h int) (uint64, error) {
	if !validTerminalGeometry(w, h) {
		return 0, errors.New("terminal geometry exceeds its allowance")
	}
	if err := s.alive(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	before := s.sequence
	s.emulator.Resize(w, h)
	s.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- s.process.Resize(w, h) }()
	select {
	case err := <-done:
		return before, err
	case <-s.ctx.Done():
		return before, s.ctx.Err()
	}
}

func (s *terminalSession) waitScreen(after uint64, predicate func(terminalScreen) bool) (terminalScreen, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var matched uint64
	var since time.Time
	for {
		if err := s.alive(); err != nil {
			return s.screen(), err
		}
		f := s.screen()
		if f.sequence > after && predicate(f) {
			if since.IsZero() || matched != f.sequence {
				matched = f.sequence
				since = time.Now()
			}
			if time.Since(since) >= 30*time.Millisecond {
				if err := ctx.Err(); err != nil {
					return f, err
				}
				return f, nil
			}
		} else {
			since = time.Time{}
		}
		select {
		case <-ctx.Done():
			return f, fmt.Errorf("expected current terminal screen was not observed: %w", ctx.Err())
		case <-s.changed:
		case <-tick.C:
		}
	}
}

func (s *terminalSession) waitExit() error {
	select {
	case <-s.exitReady:
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if s.exit.code != 0 || s.exit.err != nil {
			return errors.Join(fmt.Errorf("terminal exit %d", s.exit.code), s.exit.err)
		}
		return s.failure()
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *terminalSession) stop() (raw []byte, read int, err error) {
	s.stopping.Store(true)
	s.cancel()
	_ = s.replyClose.Close()
	// Closing the reply pipe releases a parser currently emitting a query.
	// Continue parsing while the terminal drains its final restored screen.
	ctx, cancel := context.WithTimeout(context.Background(), terminalTeardown)
	defer cancel()
	join := func(done <-chan struct{}) bool {
		select {
		case <-done:
			return true
		case <-ctx.Done():
			err = errors.Join(err, errors.New("terminal teardown did not complete"))
			return false
		}
	}
	if !join(s.exitReady) {
		_ = s.process.Kill()
	}
	closed := make(chan struct{})
	var closeErr error
	go func() { closeErr = s.process.Close(); close(closed) }()
	if join(closed) {
		err = errors.Join(err, closeErr)
	}
	for _, done := range []chan struct{}{s.outputDone, s.inputDone, s.replyDone, s.watchDone} {
		join(done)
	}
	s.mu.Lock()
	s.frozen = true
	s.mu.Unlock()
	// Read never examines the emulator's closed flag after its worker is joined.
	select {
	case <-s.replyDone:
		s.mu.Lock()
		_ = s.emulator.Close()
		s.mu.Unlock()
	default:
	}
	s.mu.Lock()
	raw = append([]byte{}, s.raw...)
	read = s.rawRead
	s.mu.Unlock()
	return raw, read, errors.Join(s.failure(), err)
}
