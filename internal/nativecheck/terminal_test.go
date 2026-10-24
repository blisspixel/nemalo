package nativecheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/term"
	"github.com/charmbracelet/x/vt"
)

func terminalHelperInvocation(t *testing.T, mode string) invocation {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"tools", "temp"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(terminalEnvironment(root), "NEMALO_TERMINAL_TEST="+mode)
	return invocation{binary: binary, directory: root, args: []string{"-test.run=^TestNativeTerminalHelper$"}, env: env}
}

func TestTerminalProcessRoundTrip(t *testing.T) {
	s, err := newTerminalSession(context.Background(), terminalHelperInvocation(t, "roundtrip"), terminalOutputLimit, startTerminal)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_, _, _ = s.stop()
		}
	})
	f, err := s.waitScreen(0, containsScreen("native-terminal-ready"))
	if err != nil {
		t.Fatal(err, s.screen().text)
	}
	if f.width != 116 || f.height != 36 {
		t.Fatal(f)
	}
	seq, err := s.send("d")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.waitScreen(seq, containsScreen("native-terminal-query-replied")); err != nil {
		t.Fatal(err, s.screen().text)
	}
	seq, err = s.resize(40, 24)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.send("r")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.waitScreen(seq, containsScreen("native-terminal-resized"))
	if err != nil {
		t.Fatal(err, s.screen().text)
	}
	_, err = s.send("q")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.waitExit(); err != nil {
		t.Fatal(err, s.screen().text)
	}
	raw, n, err := s.stop()
	stopped = true
	if err != nil || n == 0 || !bytes.Contains(raw, []byte("native-terminal-ready")) {
		t.Fatal(err, n, string(raw))
	}
	if f := s.screen(); f.alt || !strings.Contains(f.text, "native-terminal-closed") {
		t.Fatal("final output or primary screen restoration was lost", f)
	}
	for _, done := range []chan struct{}{s.exitReady, s.outputDone, s.inputDone, s.replyDone, s.watchDone} {
		select {
		case <-done:
		default:
			t.Fatal("terminal worker was not joined")
		}
	}
}

func TestTerminalProcessFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, err := newTerminalSession(ctx, terminalHelperInvocation(t, mode), terminalOutputLimit, startTerminal)
			if err != nil {
				t.Fatal(err)
			}
			stopped := false
			t.Cleanup(func() {
				if !stopped {
					_, _, _ = s.stop()
				}
			})
			_, err = s.waitScreen(0, containsScreen("native-terminal-ready"))
			if err != nil {
				t.Fatal(err, s.screen().text)
			}
			if mode == "cancel" {
				cancel()
			} else {
				_, err = s.send("q")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = s.waitExit(); err == nil {
				t.Fatal("unsuccessful terminal was accepted")
			}
			_, _, closeErr := s.stop()
			stopped = true
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			select {
			case <-s.exitReady:
			default:
				t.Fatal("owned terminal process was not reaped")
			}
		})
	}
}

func TestTerminalStartupCancellationAndBudgets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	factory := func(invocation, int, int) (terminalProcess, error) {
		called = true
		return nil, errors.New("start failed")
	}
	if _, err := newTerminalSession(ctx, invocation{}, 1024, factory); !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled acceptance started a process", err, called)
	}
	for _, limit := range []int{0, terminalOutputLimit + 1} {
		if _, err := newTerminalSession(context.Background(), invocation{}, limit, factory); err == nil || called {
			t.Fatal("invalid output limit reached startup", err, called)
		}
	}
	if _, err := newTerminalSession(context.Background(), invocation{}, 1024, factory); err == nil || !called {
		t.Fatal("failed start accepted", err, called)
	}
	for _, size := range [][2]int{{-1, 24}, {0, 24}, {121, 24}, {80, 0}, {80, 41}} {
		if validTerminalGeometry(size[0], size[1]) {
			t.Fatal("invalid geometry accepted", size)
		}
	}
}

// The helper is this suite's own Go executable, not an installed program.
// Its normal test completion preserves the toolchain's coverage publication.
func TestNativeTerminalHelper(t *testing.T) {
	mode := os.Getenv("NEMALO_TERMINAL_TEST")
	if mode == "" {
		return
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		t.Fatal("native terminal standard handles were not attached")
	}
	state, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		t.Fatal(err)
	}
	defer term.Restore(os.Stdin.Fd(), state)
	fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[2J\x1b[Hnative-terminal-ready")
	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Error(err)
			}
			return
		}
		for _, key := range buf[:n] {
			switch key {
			case 'd':
				fmt.Fprint(os.Stdout, "\x1b[6n")
			case 'R':
				fmt.Fprint(os.Stdout, "\x1b[2J\x1b[Hnative-terminal-query-replied")
			case 'f':
				fmt.Fprint(os.Stdout, strings.Repeat("bounded-output ", 1000))
			case 'r':
				fmt.Fprint(os.Stdout, "\x1b[2J\x1b[Hnative-terminal-resized")
			case 'q':
				fmt.Fprint(os.Stdout, "\x1b[?1049lnative-terminal-closed\r\n")
				if mode == "failure" {
					t.Error("requested owned helper failure")
				}
				return
			}
		}
	}
}

func TestTerminalProcessOutputAndInputLimits(t *testing.T) {
	for _, kind := range []string{"output", "input"} {
		t.Run(kind, func(t *testing.T) {
			limit := terminalOutputLimit
			if kind == "output" {
				limit = 1024
			}
			s, err := newTerminalSession(context.Background(), terminalHelperInvocation(t, "limits"), limit, startTerminal)
			if err != nil {
				t.Fatal(err)
			}
			stopped := false
			t.Cleanup(func() {
				if !stopped {
					_, _, _ = s.stop()
				}
			})
			if _, err := s.waitScreen(0, containsScreen("native-terminal-ready")); err != nil {
				t.Fatal(err)
			}
			if kind == "output" {
				_, _ = s.send("f")
			} else {
				for range terminalInputLimit / 4096 {
					if _, err := s.send(strings.Repeat("a", 4096)); err != nil {
						t.Fatal("bounded input failed", err)
					}
				}
				if _, err := s.send("a"); err == nil {
					t.Fatal("excess input accepted")
				}
			}
			if err := s.waitExit(); err == nil {
				t.Fatal("exhausted terminal accepted")
			}
			raw, n, err := s.stop()
			stopped = true
			if err == nil || !strings.Contains(err.Error(), kind+" exceeded its allowance") {
				t.Fatal("missing explicit failed allowance", err)
			}
			if len(raw) > limit || n > limit+1 {
				t.Fatal("output read or retention exceeded its bound", len(raw), n)
			}
		})
	}
}

func TestTerminalScreenUsesCurrentCells(t *testing.T) {
	e := vt.NewEmulator(40, 24)
	defer e.Close()
	s := &terminalSession{emulator: e}
	for _, value := range []string{"old evidence", "\x1b[2J\x1b[Hcurrent evidence"} {
		if _, err := e.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	f := s.screen()
	if !containsScreen("current evidence")(f) || containsScreen("old evidence")(f) {
		t.Fatal("historical output supplied the screen oracle", f.text)
	}
}

func TestTerminalProfileAndSeparateCommandDiagnostics(t *testing.T) {
	root := t.TempDir()
	env := terminalEnvironment(root)
	values := map[string]string{}
	previous := ""
	for _, v := range env {
		key, value, _ := strings.Cut(v, "=")
		key = strings.ToUpper(key)
		if key < previous {
			t.Fatal("environment was not sorted")
		}
		previous = key
		if _, ok := values[key]; ok {
			t.Fatal("duplicate profile key", key)
		}
		values[key] = value
	}
	if values["TERM"] != "xterm-256color" || values["NO_COLOR"] != "1" || values["PATH"] != filepath.Join(root, "tools") {
		t.Fatal(values)
	}
	s := testSuite(t, func(context.Context, invocation) reply {
		return reply{exit: 0, stdout: []byte(`{"schema_version":1,"command":"version","data":"v"}`)}
	})
	var setup []Command
	s.call("original", 0, nil, "version")
	s.callInto(&setup, "setup", 0, nil, "version")
	s.call("original-again", 0, nil, "version")
	if s.err != nil || len(s.result.Commands) != 2 || len(setup) != 1 || s.result.Commands[1].Stdout != "03.stdout.json" || setup[0].Stdout != "02.stdout.json" {
		t.Fatal("additional setup replaced original commands or diagnostics", s.err, s.result.Commands, setup)
	}
}
