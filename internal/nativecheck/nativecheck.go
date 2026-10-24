// Package nativecheck exercises an explicitly selected native Nemalo executable
// using only generated fixtures in a new private workspace. It is acceptance
// tooling, not another implementation of library policy.
package nativecheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

type Request struct {
	Binary, Commit, Output, Checkout string
	Terminal                         bool
}

type Command struct {
	Name         string        `json:"name"`
	Args         []string      `json:"args"`
	ExpectedExit int           `json:"expected_exit"`
	Exit         int           `json:"exit"`
	Duration     time.Duration `json:"duration_ns"`
	Stdout       string        `json:"stdout"`
	Stderr       string        `json:"stderr"`
}

type Result struct {
	Schema        string            `json:"schema"`
	Status        string            `json:"status"`
	Workspace     string            `json:"workspace"`
	Binary        Binary            `json:"binary"`
	Commands      []Command         `json:"commands"`
	FixtureSHA256 map[string]string `json:"fixture_sha256,omitempty"`
	Error         string            `json:"error,omitempty"`
	LastOutput    *CommandOutput    `json:"last_command_output,omitempty"`
	Terminal      *TerminalReport   `json:"terminal,omitempty"`
	Limitations   []string          `json:"limitations"`
}

// CommandOutput keeps a small failure excerpt in hosted logs as well as the
// complete bounded diagnostic files in the workspace. No environment block is recorded.
type CommandOutput struct {
	Name            string `json:"name"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

type suite struct {
	ctx          context.Context
	root         *os.Root
	directory    string
	binary       string
	run          runner
	env          []string
	result       Result
	err          error
	outputBytes  int
	readBytes    int64
	lastOutput   *CommandOutput
	commandCount int
}

func Run(ctx context.Context, request Request) (out Result, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return out, err
	}
	work, err := newWorkspace(request.Checkout, request.Output)
	if err != nil {
		return out, err
	}
	root := work.root
	s := newSuite(ctx, root, work.directory, execute)
	defer func() {
		if err != nil {
			s.fail(err)
		}
		if s.err == nil {
			s.result.Status = "passed"
		} else {
			s.result.Error = s.err.Error()
			s.result.LastOutput = s.lastOutput
		}
		s.result.Workspace = work.directory
		data, encodeErr := textsafe.MarshalJSON(s.result, true)
		if encodeErr == nil {
			encodeErr = root.WriteFile("receipt.json", append(data, '\n'), 0600)
		}
		err = errors.Join(s.err, encodeErr, root.Close())
		if err != nil {
			s.result.Status = "failed"
			s.result.Error = err.Error()
		}
		out = s.result
	}()
	var executable string
	s.result.Binary, executable, err = prepareBinary(root, request.Binary, request.Commit)
	if err != nil {
		return out, err
	}
	s.binary = filepath.Join(work.directory, executable)
	s.lifecycle()
	if request.Terminal && s.guard() {
		s.terminals()
	}
	return s.result, s.err
}

func newSuite(ctx context.Context, root *os.Root, directory string, run runner) *suite {
	return &suite{ctx: ctx, root: root, directory: directory, run: run, env: childEnvironment(directory), result: Result{Schema: "nemalo-native-check/1", Status: "failed", Commands: []Command{}, Limitations: []string{
		"Synthetic fixture acceptance on this native OS and architecture, not a release or whole-format conformance.",
		"External tool discovery is disabled. No installed antivirus, decoder, provider, external reader or model is exercised. Holdings must remain in review.",
		"macOS default state paths are computed but unused by these selected commands; explicit configuration and scratch stay in the owned workspace. HOME is unchanged.",
		"Existing crash/recovery, Windows boundary, installer, real host and interactive terminal gates remain separate and are not replaced.",
	}}}
}

func (s *suite) fail(err error) {
	if s.err == nil && err != nil {
		s.err = err
	}
}
func (s *suite) guard() bool { s.fail(s.ctx.Err()); return s.err == nil }
func (s *suite) require(ok bool, why string) {
	if !ok {
		s.fail(errors.New(why))
	}
}
func (s *suite) path(name string) string {
	return filepath.Join(s.directory, filepath.FromSlash(name))
}
func (s *suite) write(name string, b []byte) {
	if !s.guard() {
		return
	}
	s.fail(s.root.WriteFile(name, b, 0600))
}
func (s *suite) mkdir(name string) {
	if !s.guard() {
		return
	}
	s.fail(s.root.Mkdir(name, 0700))
}
func (s *suite) remove(name string) {
	if !s.guard() {
		return
	}
	s.fail(s.root.Remove(name))
}
func (s *suite) read(name string) []byte {
	if !s.guard() {
		return nil
	}
	before, err := s.root.Lstat(name)
	if err != nil {
		s.fail(err)
		return nil
	}
	f, err := safeio.OpenRegular(s.root, name, before, os.O_RDONLY)
	if err != nil {
		s.fail(err)
		return nil
	}
	if before.Size() > 1<<20 {
		s.fail(errors.Join(errors.New("fixture read exceeds its allowance"), f.Close()))
		return nil
	}
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	s.fail(errors.Join(err, f.Close()))
	s.readBytes += int64(len(b))
	if len(b) > 1<<20 || s.readBytes > 64<<20 {
		s.fail(errors.New("fixture read allowance exceeded"))
		return nil
	}
	return b
}

func (s *suite) call(name string, want int, dst any, args ...string) {
	s.callInto(&s.result.Commands, name, want, dst, args...)
}

func (s *suite) callInto(commands *[]Command, name string, want int, dst any, args ...string) {
	if s.err != nil {
		return
	}
	if err := s.ctx.Err(); err != nil {
		s.fail(err)
		return
	}
	args = append(append([]string{}, args...), "--json")
	started := time.Now()
	in, err := s.checkedInvocation(args)
	if err != nil {
		s.fail(err)
		return
	}
	r := s.run(s.ctx, in)
	s.lastOutput = &CommandOutput{Name: name, Stdout: string(r.stdout[:min(len(r.stdout), 16<<10)]), Stderr: string(r.stderr[:min(len(r.stderr), 4<<10)]), StdoutTruncated: len(r.stdout) > 16<<10, StderrTruncated: len(r.stderr) > 4<<10}
	s.commandCount++
	index := s.commandCount
	stdout, stderr := fmt.Sprintf("%02d.stdout.json", index), fmt.Sprintf("%02d.stderr.txt", index)
	*commands = append(*commands, Command{Name: name, Args: args, ExpectedExit: want, Exit: r.exit, Duration: time.Since(started), Stdout: stdout, Stderr: stderr})
	s.outputBytes += len(r.stdout) + len(r.stderr)
	if len(r.stdout) > maxStdout || len(r.stderr) > maxStderr || s.outputBytes > 8<<20 {
		s.fail(errors.New("native acceptance diagnostic budget exceeded"))
		return
	}
	// Diagnostics are retained after a child timeout or cancellation; only
	// fixture/application work is suppressed by the cancellation guard.
	s.fail(errors.Join(s.root.WriteFile(stdout, r.stdout, 0600), s.root.WriteFile(stderr, r.stderr, 0600)))
	if s.err != nil {
		return
	}
	if r.err != nil {
		s.fail(fmt.Errorf("%s: %w", name, r.err))
		return
	}
	if r.exit != want {
		s.fail(fmt.Errorf("%s: exit %d, expected %d; retained diagnostics %s", name, r.exit, want, stdout))
		return
	}
	var env struct {
		SchemaVersion int             `json:"schema_version"`
		Command       string          `json:"command"`
		Data          json.RawMessage `json:"data"`
		Error         string          `json:"error"`
	}
	if err := json.Unmarshal(r.stdout, &env); err != nil {
		s.fail(fmt.Errorf("%s: invalid JSON envelope: %w", name, err))
		return
	}
	if env.SchemaVersion != 1 || env.Command != args[0] || (want == 0 || dst != nil) && (len(env.Data) == 0 || string(env.Data) == "null") || want == 0 && env.Error != "" || want != 0 && env.Error == "" {
		s.fail(fmt.Errorf("%s: inconsistent JSON envelope", name))
		return
	}
	if dst != nil {
		value := reflect.ValueOf(dst)
		if value.Kind() != reflect.Pointer || value.IsNil() {
			s.fail(errors.New("result destination must be a non-nil pointer"))
			return
		}
		fresh := reflect.New(value.Elem().Type())
		if err := json.Unmarshal(env.Data, fresh.Interface()); err != nil {
			s.fail(fmt.Errorf("%s: invalid result: %w", name, err))
			return
		}
		value.Elem().Set(fresh.Elem())
	}
}

func (s *suite) checkedInvocation(args []string) (invocation, error) {
	opened, err := s.root.Stat(".")
	if err != nil {
		return invocation{}, err
	}
	named, err := safeio.Stat(s.directory)
	if err != nil {
		return invocation{}, err
	}
	if !os.SameFile(opened, named) {
		return invocation{}, errors.New("workspace pathname changed before child launch")
	}
	return invocation{binary: s.binary, directory: s.directory, args: args, env: s.env}, nil
}

type fileFact struct {
	Mode   string
	Size   int64
	SHA256 string
}

func (s *suite) snapshot(name string) map[string]fileFact {
	if !s.guard() {
		return nil
	}
	root, err := s.root.OpenRoot(name)
	if err != nil {
		s.fail(err)
		return nil
	}
	defer func() { s.fail(root.Close()) }()
	facts := map[string]fileFact{}
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if len(facts) >= 1024 {
			return errors.New("fixture inventory limit exceeded")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		fact := fileFact{Mode: info.Mode().String()}
		if !d.IsDir() {
			if !info.Mode().IsRegular() || info.Size() > 1<<20 {
				return errors.New("unexpected fixture object or size")
			}
			f, err := safeio.OpenRegular(root, p, info, os.O_RDONLY)
			if err != nil {
				return err
			}
			h := sha256.New()
			n, copyErr := io.Copy(h, io.LimitReader(f, info.Size()+1))
			err = errors.Join(copyErr, f.Close())
			if err != nil {
				return err
			}
			s.readBytes += n
			if s.readBytes > 64<<20 || n != info.Size() {
				return errors.New("fixture inventory byte allowance or identity changed")
			}
			fact.Size, fact.SHA256 = n, hex.EncodeToString(h.Sum(nil))
		}
		facts[p] = fact
		return nil
	})
	s.fail(err)
	return facts
}

func (s *suite) unchanged(name string, before map[string]fileFact) {
	after := s.snapshot(name)
	s.require(reflect.DeepEqual(before, after), name+": bytes or modes changed during a read-only/refused operation")
}
