package nativecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/cli"
	"github.com/blisspixel/nemalo/internal/config"
	"github.com/blisspixel/nemalo/internal/decode"
	"github.com/blisspixel/nemalo/internal/scanner"
)

type missingScanner struct{ calls *int }

func (s missingScanner) Scan(context.Context, string) scanner.Result {
	*s.calls++
	return scanner.Result{Status: "unavailable", ExitCode: -1}
}

func testSuite(t *testing.T, run runner) *suite {
	t.Helper()
	base := t.TempDir()
	work, err := newWorkspace(t.TempDir(), filepath.Join(base, "workspace space 日本"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := work.root.Close(); err != nil {
			t.Error(err)
		}
	})
	return newSuite(context.Background(), work.root, work.directory, run)
}

func TestLifecycleThroughRealCLIWithOfflineAdapters(t *testing.T) {
	// Execute's injected configuration reader does not replace os/exec lookups.
	// Hide tools in this test process too; no installed adapter may be selected.
	tools := filepath.Join(t.TempDir(), "empty-tools")
	for _, key := range []string{"PATH", "ProgramData", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "CommonProgramFiles", "CommonProgramFiles(x86)", "CommonProgramW6432", "ALLUSERSPROFILE"} {
		t.Setenv(key, tools)
	}
	var names []string
	scans := 0
	s := testSuite(t, func(ctx context.Context, in invocation) reply {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("lifecycle caller has no deadline")
		}
		env := map[string]string{}
		for _, v := range in.env {
			k, v, _ := strings.Cut(v, "=")
			env[k] = v
		}
		var stdout, stderr bytes.Buffer
		service := app.Service{Scanner: missingScanner{&scans}, PCMDecoder: func(context.Context, string, string, int64, decode.PCMRequest) (decode.PCMRange, error) {
			t.Fatal("native lifecycle decoded audio")
			return decode.PCMRange{}, errors.New("unexpected decoder")
		}}
		code := cli.Execute(ctx, in.args, &stdout, &stderr, service, func(k string) string { return env[k] }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(in.directory, "unused-config"), State: filepath.Join(in.directory, "state"), Cache: filepath.Join(in.directory, "cache")}, nil
		}, func(context.Context, app.Service) error {
			t.Fatal("native lifecycle opened a terminal")
			return errors.New("unexpected terminal")
		})
		return reply{stdout: stdout.Bytes(), stderr: stderr.Bytes(), exit: code}
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s.ctx = ctx
	s.lifecycle()
	if s.err != nil {
		t.Fatal(s.err)
	}
	for _, c := range s.result.Commands {
		names = append(names, c.Name)
		if c.Exit != c.ExpectedExit {
			t.Fatal("wrong exit", c)
		}
	}
	if !reflect.DeepEqual(names, requiredCases) || len(names) != 39 || scans != 1 {
		t.Fatal("missing cases or unexpected scanner use", names, scans)
	}
	if _, err := os.Stat(filepath.Join(s.directory, "cleanup-source", "uncertain.bin")); err != nil {
		t.Fatal(err)
	}
	var health struct {
		Data struct {
			Healthy           bool
			Checked, Verified int
		}
	}
	b, err := os.ReadFile(filepath.Join(s.directory, "39.stdout.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &health); err != nil || !health.Data.Healthy || health.Data.Checked != 0 || health.Data.Verified != 2 {
		t.Fatal("fixture bytes became checked", health, err)
	}
}

func TestChildEnvironmentDoesNotInheritToolRoots(t *testing.T) {
	root := t.TempDir()
	env := isolatedEnvironment(root, []string{"HOME=original-home", "Path=host-tools", "PROGRAMDATA=host-data", "ProgramFiles=host-programs", "ProgramFiles(x86)=host-x86", "ProgramW6432=host-native", "CommonProgramFiles=host-common", "CommonProgramFiles(x86)=host-common-x86", "CommonProgramW6432=host-common-native", "ALLUSERSPROFILE=host-profile", "nEmAlO_CONFIG=host-config", "xdg_cache_home=host-cache"})
	values := map[string]string{}
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		key = strings.ToUpper(key)
		if _, found := values[key]; found {
			t.Fatal("duplicate environment key", key)
		}
		values[key] = value
	}
	for _, key := range []string{"PATH", "PROGRAMDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "PROGRAMW6432", "COMMONPROGRAMFILES", "COMMONPROGRAMFILES(X86)", "COMMONPROGRAMW6432", "ALLUSERSPROFILE"} {
		if values[key] != filepath.Join(root, "tools") {
			t.Errorf("host tool root inherited: %s=%q", key, values[key])
		}
	}
	if values["HOME"] != "original-home" || values["NEMALO_CONFIG"] != filepath.Join(root, "config.json") || values["XDG_CACHE_HOME"] != filepath.Join(root, "cache") {
		t.Fatal("home or explicit configuration changed", values)
	}
}

func TestChildEnvironmentInNativeProcess(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"tools", "temp"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := execute(context.Background(), invocation{binary: binary, directory: root, args: []string{"-test.run=^TestNativeProcessHelper$"}, env: append(childEnvironment(root), "NEMALO_NATIVE_HELPER=environment")})
	if r.err != nil || r.exit != 0 {
		t.Fatal("native environment helper failed", r.err, r.exit, string(r.stderr))
	}
	var got struct {
		Environment map[string]string
		Scanner     string
		Tools       map[string]string
	}
	if err := json.NewDecoder(bytes.NewReader(r.stdout)).Decode(&got); err != nil {
		t.Fatal(err, string(r.stdout))
	}
	for _, key := range []string{"PATH", "ProgramData", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "CommonProgramFiles", "CommonProgramFiles(x86)", "CommonProgramW6432", "ALLUSERSPROFILE"} {
		if got.Environment[key] != filepath.Join(root, "tools") {
			t.Errorf("native child tool root changed: %s=%q", key, got.Environment[key])
		}
	}
	if got.Scanner != "" || len(got.Tools) != 0 {
		t.Fatal("native child discovered an installed adapter", got, string(r.stdout))
	}
}

func TestToolIsolationRefusalPrecedesLibraryInitialization(t *testing.T) {
	for _, capabilities := range []string{`[{"name":"clamscan","available":false},{"name":"MpCmdRun.exe","available":true}]`, `[{"name":"clamscan","available":false}]`} {
		s := testSuite(t, func(_ context.Context, in invocation) reply {
			if in.args[0] == "version" {
				return reply{exit: 0, stdout: []byte(`{"schema_version":1,"command":"version","data":"v"}`)}
			}
			if in.args[0] != "doctor" {
				t.Fatal("tool isolation refusal still launched an application operation", in.args)
			}
			return reply{exit: 0, stdout: []byte(`{"schema_version":1,"command":"doctor","data":{"security_status":"not_scanned","configuration":{},"capabilities":` + capabilities + `}}`)}
		})
		s.lifecycle()
		if s.err == nil || len(s.result.Commands) != 2 || s.lastOutput.Name != "doctor" || !strings.Contains(s.lastOutput.Stdout, "capabilities") {
			t.Fatal("missing early refusal or its diagnostic evidence", s.err, s.result.Commands, s.lastOutput)
		}
		if _, err := os.Stat(filepath.Join(s.directory, "library", ".nemalo")); !os.IsNotExist(err) {
			t.Fatal("tool isolation refusal still initialized a library", err)
		}
	}
}

func TestFailureStopsAllLaterCommandsAndFixtureMutation(t *testing.T) {
	for _, which := range []string{"transport", "exit", "json", "schema", "command", "missing-data", "error", "budget", "cancelled"} {
		t.Run(which, func(t *testing.T) {
			calls := 0
			s := testSuite(t, func(ctx context.Context, in invocation) reply {
				calls++
				r := reply{exit: 0, stdout: []byte(`{"schema_version":1,"command":"version","data":"version"}`)}
				switch which {
				case "transport":
					r.err = errors.New("process failed")
				case "exit":
					r.exit = 1
				case "json":
					r.stdout = []byte("{")
				case "schema":
					r.stdout = []byte(`{"schema_version":2,"command":"version","data":"v"}`)
				case "command":
					r.stdout = []byte(`{"schema_version":1,"command":"other","data":"v"}`)
				case "missing-data":
					r.stdout = []byte(`{"schema_version":1,"command":"version"}`)
				case "error":
					r.stdout = []byte(`{"schema_version":1,"command":"version","data":"v","error":"failure"}`)
				case "budget":
					r.stdout = bytes.Repeat([]byte("x"), maxStdout+1)
				case "cancelled":
					r.err = context.Canceled
				}
				return r
			})
			s.lifecycle()
			if s.err == nil || calls != 1 || len(s.result.Commands) != 1 {
				t.Fatal("failed run continued", s.err, calls)
			}
			if s.lastOutput == nil || s.lastOutput.Name != "version" || len(s.lastOutput.Stdout) > 16<<10 || len(s.lastOutput.Stderr) > 4<<10 || s.lastOutput.StdoutTruncated != (which == "budget") {
				t.Fatal("missing or unbounded failure excerpt", s.lastOutput)
			}
			if _, err := os.Stat(filepath.Join(s.directory, "library", ".nemalo")); !os.IsNotExist(err) {
				t.Fatal("failed version still initialized a library", err)
			}
			original, err := bookFixture("Knowledge is power. " + strings.Repeat("Synthetic 日本語 passage. ", 400))
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(s.directory, "source", "book space 日本.epub"))
			if err != nil || !bytes.Equal(data, original) {
				t.Fatal("failure still mutated a fixture", err)
			}
		})
	}
}

func TestEnvelopeRefusalWithoutDataAndFreshDecode(t *testing.T) {
	responses := []reply{{exit: 1, stdout: []byte(`{"schema_version":1,"command":"bookmark","error":"disabled"}`)}, {exit: 0, stdout: []byte(`{"schema_version":1,"command":"content","data":{"value":"first","other":"old"}}`)}, {exit: 0, stdout: []byte(`{"schema_version":1,"command":"content","data":{"value":"second"}}`)}}
	i := 0
	s := testSuite(t, func(context.Context, invocation) reply { r := responses[i]; i++; return r })
	s.call("refused", 1, nil, "bookmark", "save")
	var value struct{ Value, Other string }
	s.call("first", 0, &value, "content", "read")
	s.call("second", 0, &value, "content", "read")
	if s.err != nil || value.Value != "second" || value.Other != "" {
		t.Fatal("omitted fields reused earlier data", value, s.err)
	}
	for _, dst := range []any{42, (*string)(nil), new(int)} {
		s := testSuite(t, func(context.Context, invocation) reply {
			return reply{exit: 0, stdout: []byte(`{"schema_version":1,"command":"version","data":"not-an-int"}`)}
		})
		s.call("bad-type", 0, dst, "version")
		if s.err == nil {
			t.Fatal("bad destination accepted")
		}
	}
}

func TestWorkspaceScopeAndFreshOwnership(t *testing.T) {
	repo, parent := t.TempDir(), t.TempDir()
	for _, output := range []string{repo, filepath.Join(repo, "new"), "relative", parent, filepath.Join(parent, "missing", "new")} {
		if w, err := newWorkspace(repo, output); err == nil {
			w.root.Close()
			t.Fatal("unsafe output accepted", output)
		}
	}
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal("native symlink required", err)
	}
	if w, err := newWorkspace(repo, filepath.Join(alias, "new")); err == nil {
		w.root.Close()
		t.Fatal("checkout alias accepted")
	}
	if _, err := os.Stat(filepath.Join(repo, "new")); !os.IsNotExist(err) {
		t.Fatal("refusal created checkout output", err)
	}
	w, err := newWorkspace(repo, filepath.Join(parent, "owned"))
	if err != nil {
		t.Fatal(err)
	}
	if err = w.root.WriteFile("bound", []byte("bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(w.directory, "bound")); err != nil || string(b) != "bytes" {
		t.Fatal("public path not bound to capability", err)
	}
	if err = w.root.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = newWorkspace(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(w.directory) })
	if err = w.root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := outsideCheckout(filepath.Join(parent, "missing"), parent); err == nil {
		t.Fatal("missing checkout accepted")
	}
}

func TestCheckoutFindsOwningModuleBeforeOutputValidation(t *testing.T) {
	repo := t.TempDir()
	nested := filepath.Join(repo, "nested", "source")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "go.mod")
	if err := os.WriteFile(file, []byte("module github.com/blisspixel/nemalo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Checkout(nested)
	if err != nil {
		t.Fatal(err)
	}
	a, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(repo)
	if err != nil || !os.SameFile(a, b) {
		t.Fatal(got, err)
	}
	if w, err := newWorkspace(got, filepath.Join(repo, "elsewhere")); err == nil {
		w.root.Close()
		t.Fatal("nested invocation accepted checkout output")
	}
	for _, data := range [][]byte{[]byte("module other\n"), []byte("go 1.27.3\n"), bytes.Repeat([]byte("x"), (64<<10)+1)} {
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Checkout(nested); err == nil {
			t.Fatal("invalid marker accepted")
		}
	}
	if _, err := Checkout(filepath.Join(repo, "missing")); err == nil {
		t.Fatal("missing source directory accepted")
	}
}

func TestWorkspaceIdentityAndSnapshotRefusals(t *testing.T) {
	s := testSuite(t, func(context.Context, invocation) reply { t.Fatal("changed workspace launched a child"); return reply{} })
	s.directory = t.TempDir()
	s.call("changed", 0, nil, "version")
	if s.err == nil {
		t.Fatal("changed identity accepted")
	}
	for _, which := range []string{"large", "link", "budget", "missing", "cancelled"} {
		t.Run(which, func(t *testing.T) {
			s := testSuite(t, nil)
			s.mkdir("data")
			switch which {
			case "large":
				s.write("data/file", make([]byte, (1<<20)+1))
			case "link":
				if err := os.Symlink(filepath.Join(t.TempDir(), "target"), filepath.Join(s.directory, "data", "link")); err != nil {
					t.Fatal(err)
				}
			case "budget":
				s.write("data/file", []byte("x"))
				s.readBytes = 64 << 20
			case "missing":
				s.snapshot("absent")
			case "cancelled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				s.ctx = ctx
			}
			s.snapshot("data")
			if s.err == nil {
				t.Fatal("unsafe inventory accepted", which)
			}
		})
	}
}

func TestBuildIdentityAndInvalidBinaryRetainFailure(t *testing.T) {
	commit := strings.Repeat("a", 40)
	valid := func() *debug.BuildInfo {
		return &debug.BuildInfo{GoVersion: runtime.Version(), Path: "github.com/blisspixel/nemalo/cmd/nemalo", Main: debug.Module{Path: "github.com/blisspixel/nemalo"}, Settings: []debug.BuildSetting{{Key: "GOOS", Value: runtime.GOOS}, {Key: "GOARCH", Value: runtime.GOARCH}, {Key: "CGO_ENABLED", Value: "0"}, {Key: "vcs.revision", Value: commit}, {Key: "vcs.modified", Value: "false"}}}
	}
	if b, err := validateBuild(valid(), commit); err != nil || !b.Clean {
		t.Fatal(b, err)
	}
	for _, which := range []string{"sha", "module", "path", "version", "replace", "dependency", "duplicate", "GOOS", "GOARCH", "CGO_ENABLED", "vcs.revision", "vcs.modified"} {
		t.Run(which, func(t *testing.T) {
			info := valid()
			sha := commit
			switch which {
			case "sha":
				sha = "ABC"
			case "module":
				info.Main.Path = "other"
			case "path":
				info.Path = "other"
			case "version":
				info.GoVersion = "go0"
			case "replace":
				info.Main.Replace = &debug.Module{Path: "other"}
			case "dependency":
				info.Deps = []*debug.Module{{Path: "dependency", Replace: &debug.Module{Path: "other"}}}
			case "duplicate":
				info.Settings = append(info.Settings, info.Settings[0])
			default:
				for i := range info.Settings {
					if info.Settings[i].Key == which {
						info.Settings[i].Value = "wrong"
					}
				}
			}
			if _, err := validateBuild(info, sha); err == nil {
				t.Fatal("wrong binary identity accepted", which)
			}
		})
	}
	base := t.TempDir()
	binary := filepath.Join(base, "not-a-binary")
	if err := os.WriteFile(binary, []byte("not executable"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(base, "result")
	r, err := Run(context.Background(), Request{Binary: binary, Commit: commit, Checkout: t.TempDir(), Output: output})
	if err == nil || r.Status != "failed" || len(r.Commands) != 0 {
		t.Fatal("invalid binary executed", r, err)
	}
	if _, err = os.Stat(filepath.Join(output, "receipt.json")); err != nil {
		t.Fatal("failure receipt lost", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(base, "cancelled")
	if _, err = Run(ctx, Request{Binary: binary, Commit: commit, Checkout: base, Output: out}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("cancelled run created output", err)
	}
}

func TestProcessOutputAndCancellation(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ok", "exit", "stdout", "stderr", "wait"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			cancel := func() {}
			if mode == "wait" {
				ctx, cancel = context.WithTimeout(ctx, 300*time.Millisecond)
			}
			defer cancel()
			r := execute(ctx, invocation{binary: binary, directory: t.TempDir(), args: []string{"-test.run=^TestNativeProcessHelper$"}, env: append(os.Environ(), "NEMALO_NATIVE_HELPER="+mode)})
			switch mode {
			case "ok":
				if r.err != nil || r.exit != 0 || !bytes.Contains(r.stdout, []byte("native helper")) {
					t.Fatal(r)
				}
			case "exit":
				if r.err != nil || r.exit != 7 {
					t.Fatal(r)
				}
			case "wait":
				if !errors.Is(r.err, context.DeadlineExceeded) {
					t.Fatal(r)
				}
			default:
				if r.err == nil || !strings.Contains(r.err.Error(), "allowance") || len(r.stdout) > maxStdout || len(r.stderr) > maxStderr {
					t.Fatal("output limit accepted", len(r.stdout), len(r.stderr), r.err)
				}
			}
		})
	}
	r := execute(context.Background(), invocation{binary: filepath.Join(t.TempDir(), "missing"), directory: t.TempDir()})
	if r.err == nil {
		t.Fatal("failed start accepted")
	}
}

func TestNativeProcessHelper(t *testing.T) {
	switch os.Getenv("NEMALO_NATIVE_HELPER") {
	case "environment":
		values := map[string]string{}
		for _, key := range []string{"PATH", "ProgramData", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "CommonProgramFiles", "CommonProgramFiles(x86)", "CommonProgramW6432", "ALLUSERSPROFILE"} {
			values[key] = os.Getenv(key)
		}
		_, resolvedScanner, _ := scanner.Resolve() // Resolve only; never execute it.
		tools := map[string]string{}
		for _, name := range []string{"7z", "7zz", "7za", "ffmpeg", "ffprobe", "epubcheck"} {
			if path, err := exec.LookPath(name); err == nil {
				tools[name] = path
			}
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"Environment": values, "Scanner": resolvedScanner, "Tools": tools}); err != nil {
			t.Fatal(err)
		}
	case "ok":
		os.Stdout.WriteString("native helper")
	case "exit":
		os.Exit(7)
	case "stdout":
		os.Stdout.Write(bytes.Repeat([]byte("x"), maxStdout+4096))
	case "stderr":
		os.Stderr.Write(bytes.Repeat([]byte("x"), maxStderr+4096))
	case "wait":
		time.Sleep(time.Minute)
	}
}
