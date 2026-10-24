package nativecheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fakeTool creates a never-executed file that os/exec lookup would accept.
func fakeTool(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("not executed: "+name), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeTools(t *testing.T) (InstalledTools, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tools space")
	tools := InstalledTools{SevenZip: fakeTool(t, dir, "7z"), FFmpeg: fakeTool(t, dir, "ffmpeg"), FFprobe: fakeTool(t, dir, "ffprobe")}
	if runtime.GOOS == "windows" {
		tools.Defender = fakeTool(t, filepath.Join(dir, "Windows Defender"), "MpCmdRun")
	} else {
		tools.ClamScan = fakeTool(t, dir, "clamscan")
	}
	return tools, dir
}

func TestInstalledSelectionRefusals(t *testing.T) {
	tools, dir := fakeTools(t)
	checkout, workspace := t.TempDir(), t.TempDir()
	base := []string{"PATH=" + t.TempDir(), "KEEP=1"}
	if _, err := selectInstalled(tools, checkout, workspace, base, runtime.GOOS); err != nil {
		t.Fatal(err)
	}
	inside := fakeTool(t, filepath.Join(workspace, "bin"), "ffmpeg")
	shadow := t.TempDir()
	cases := map[string]func(*InstalledTools){
		"missing":          func(x *InstalledTools) { x.FFprobe = "" },
		"relative":         func(x *InstalledTools) { x.FFmpeg = "ffmpeg" },
		"absent":           func(x *InstalledTools) { x.FFmpeg = filepath.Join(dir, "absent") },
		"directory":        func(x *InstalledTools) { x.FFmpeg = dir },
		"inside workspace": func(x *InstalledTools) { x.FFmpeg = inside; x.FFprobe = fakeTool(t, filepath.Dir(inside), "ffprobe") },
		"inside checkout":  func(x *InstalledTools) { x.SevenZip = fakeTool(t, checkout, "7z") },
		// 7zz precedes 7z in Nemalo's lookup, so a 7zz beside ffmpeg would be
		// run instead of the selected 7z.
		"shadowed 7-zip": func(x *InstalledTools) {
			x.FFmpeg, x.FFprobe = fakeTool(t, shadow, "ffmpeg"), fakeTool(t, shadow, "ffprobe")
			fakeTool(t, shadow, "7zz")
		},
	}
	if runtime.GOOS == "windows" {
		cases["clamscan on windows"] = func(x *InstalledTools) { x.ClamScan = x.FFmpeg }
		cases["no defender"] = func(x *InstalledTools) { x.Defender = "" }
		cases["unresolvable defender"] = func(x *InstalledTools) { x.Defender = fakeTool(t, t.TempDir(), "MpCmdRun") }
	} else {
		cases["defender off windows"] = func(x *InstalledTools) { x.Defender = x.FFmpeg }
		cases["no clamscan"] = func(x *InstalledTools) { x.ClamScan = "" }
		cases["not executable"] = func(x *InstalledTools) {
			p := filepath.Join(t.TempDir(), "clamscan")
			if err := os.WriteFile(p, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			x.ClamScan = p
		}
	}
	for name, change := range cases {
		x := tools
		change(&x)
		if sel, err := selectInstalled(x, checkout, workspace, base, runtime.GOOS); err == nil {
			t.Errorf("%s: accepted %+v", name, sel)
		}
	}
}

func TestInstalledEnvironmentNamesOnlySelectedDirectories(t *testing.T) {
	tools, dir := fakeTools(t)
	other := t.TempDir()
	// The first directory's ffprobe would otherwise shadow this selection.
	if err := os.Remove(tools.FFprobe); err != nil {
		t.Fatal(err)
	}
	tools.FFprobe = fakeTool(t, other, "ffprobe")
	base := []string{"PATH=" + t.TempDir(), "KEEP=1", "ProgramData=" + t.TempDir()}
	sel, err := selectInstalled(tools, t.TempDir(), t.TempDir(), base, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sel.path, []string{dir, other}) || envValue(sel.env, "PATH", runtime.GOOS) != dir+string(os.PathListSeparator)+other || envValue(sel.env, "KEEP", runtime.GOOS) != "1" {
		t.Fatal(sel.path, sel.env)
	}
	if runtime.GOOS == "windows" && envValue(sel.env, "ProgramFiles", runtime.GOOS) != dir {
		t.Fatal("Defender root was not restored", sel.env)
	}
}

func TestDefenderRootsMatchNemaloResolution(t *testing.T) {
	data := filepath.Join(string(filepath.Separator)+"pd", "Microsoft", "Windows Defender", "Platform", "4.18.1-0", "MpCmdRun.exe")
	if key, root, err := defenderRoot(data); err != nil || key != "ProgramData" || root != string(filepath.Separator)+"pd" {
		t.Fatal(key, root, err)
	}
	s := testSuite(t, nil)
	if err := os.MkdirAll(filepath.Join(s.directory, "pd", "Microsoft", "Windows Defender", "Platform", "4.18.1-0"), 0o700); err != nil {
		t.Fatal(err)
	}
	platform := filepath.Join(s.directory, "pd", "Microsoft", "Windows Defender", "Platform", "4.18.1-0", "MpCmdRun.exe")
	if err := os.WriteFile(platform, []byte("not executed"), 0o600); err != nil {
		t.Fatal(err)
	}
	facts := s.toolFacts(&installedSelection{tools: []selectedTool{{role: "defender", path: platform}}, goos: "windows"})
	if s.err != nil || len(facts) != 1 || facts[0].Version != "Microsoft Defender platform 4.18.1-0" || facts[0].Bytes != int64(len("not executed")) {
		t.Fatal(facts, s.err)
	}
	files := filepath.Join(string(filepath.Separator)+"pf", "Windows Defender", "MpCmdRun.exe")
	if key, root, err := defenderRoot(files); err != nil || key != "ProgramFiles" || root != string(filepath.Separator)+"pf" {
		t.Fatal(key, root, err)
	}
	for _, bad := range []string{filepath.Join(string(filepath.Separator)+"x", "MpCmdRun.exe"), filepath.Join(string(filepath.Separator)+"pf", "Windows Defender", "other.exe")} {
		if _, _, err := defenderRoot(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
}

func TestWAVDataAndVersionLine(t *testing.T) {
	samples := []byte{1, 2, 3, 4}
	if got, err := wavData(wav(samples)); err != nil || !bytes.Equal(got, samples) {
		t.Fatal(got, err)
	}
	for _, bad := range [][]byte{nil, []byte("RIFF....WAVX"), wav(samples)[:46]} {
		if _, err := wavData(bad); err == nil {
			t.Fatal("accepted", bad)
		}
	}
	if got := versionLine([]byte("\r\n\x1b[1mTool 1.0\x00\r\nmore")); got != "[1mTool 1.0" {
		t.Fatalf("%q", got)
	}
	if got := versionLine([]byte(strings.Repeat("é", 300))); len(got) > 200 || !strings.HasPrefix(got, "é") {
		t.Fatalf("%q", got)
	}
	if hex.EncodeToString(sum(eicar())) != eicarSHA256 || len(eicar()) != 68 {
		t.Fatal("antivirus test file assembly changed")
	}
}

func sum(b []byte) []byte { d := sha256.Sum256(b); return d[:] }

func wav(samples []byte) []byte {
	b := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x10\x00\x00\x00")
	b = append(b, make([]byte, 16)...)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(samples)))
	return append(b, samples...)
}

// simulatedInstall answers the harness's tool runs and Nemalo commands from
// fixed behavior. fault names one behavior to break.
type simulatedInstall struct {
	t        *testing.T
	s        *suite
	sel      *installedSelection
	tools    InstalledTools
	fault    string
	nemalo   []string
	toolRuns int
}

func (f *simulatedInstall) reference() []byte {
	b := make([]byte, 4000)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

func (f *simulatedInstall) run(_ context.Context, in invocation) reply {
	if in.binary != f.s.binary {
		f.toolRuns++
		last := ""
		if len(in.args) > 0 {
			last = in.args[len(in.args)-1]
		}
		switch {
		case len(in.args) > 0 && in.args[0] == "a":
			_ = os.WriteFile(in.args[5], []byte("7z\xbc\xaf\x27\x1carchive"), 0o600)
		case slices.Contains(in.args, "lavfi"):
			_ = os.WriteFile(last, []byte("synthetic m4a"), 0o600)
		case slices.Contains(in.args, "s16le"):
			_ = os.WriteFile(last, f.reference(), 0o600)
		}
		return reply{stdout: []byte("Tool 1.0\n"), exit: 0}
	}
	f.nemalo = append(f.nemalo, strings.Join(in.args[:min(2, len(in.args))], " "))
	installed := envValue(in.env, "PATH", runtime.GOOS) == envValue(f.sel.env, "PATH", runtime.GOOS)
	scanner := f.tools.ClamScan + f.tools.Defender
	ok := func(data any) reply { return f.envelope(in.args[0], data, "", 0) }
	fail := func(data any) reply { return f.envelope(in.args[0], data, "refused", 1) }
	arg := strings.Join(in.args, " ")
	switch {
	case in.args[0] == "doctor":
		return ok(map[string]any{"security_status": "not_scanned", "signatures": map[string]any{"tool": "clamav", "status": "current"}, "capabilities": []map[string]any{
			{"name": "clamscan", "available": installed}, {"name": "MpCmdRun.exe", "available": installed}, {"name": "7z", "available": installed}, {"name": "ffmpeg", "available": installed}, {"name": "ffprobe", "available": installed}}})
	case strings.HasSuffix(in.args[1], "eicar.txt"):
		if f.fault == "scanner removes test file" {
			_ = os.Remove(in.args[1])
		}
		return fail(map[string]any{"antivirus": map[string]any{"status": "findings", "tool": scanner}})
	case in.args[0] == "check" && strings.HasSuffix(in.args[1], ".epub"):
		return ok(map[string]any{"detected_format": "epub", "antivirus": map[string]any{"status": "no_detections_reported", "tool": scanner}})
	case in.args[0] == "check" && !installed:
		return fail(map[string]any{"decode": map[string]any{"status": "unavailable"}})
	case in.args[0] == "check":
		exe := f.tools.FFmpeg
		if f.fault == "other decoder" {
			exe = filepath.Join(filepath.Dir(exe), "elsewhere")
		}
		return ok(map[string]any{"detected_format": "m4a", "decode": map[string]any{"status": "passed", "executable": exe, "decoded_samples": 2000}})
	case strings.HasPrefix(arg, "library init"):
		return ok(map[string]any{})
	case strings.HasPrefix(arg, "library ingest") && !installed:
		return ok(map[string]any{"complete": true, "not_imported": 1, "items": []map[string]any{{"outcome": "not_imported", "detail": "7-Zip is not installed"}}})
	case strings.HasPrefix(arg, "library ingest"):
		checked := 1
		if f.fault == "unchecked member" {
			checked = 0
		}
		return ok(map[string]any{"complete": true, "imported": 1, "checked": checked})
	case strings.HasPrefix(arg, "library import") && !slices.Contains(in.args, "--apply"):
		return ok(map[string]any{"plan": "plan"})
	case strings.HasPrefix(arg, "library import"):
		return ok(map[string]any{"applied": true, "asset_id": "sha256:aa", "status": "review"})
	case strings.HasPrefix(arg, "content units"):
		ref := f.reference()
		return ok(map[string]any{"mp4_pcm": map[string]any{"output_sample_rate": 44100, "output_channels": 1, "total_samples": "2000", "full_pcm_sha256": hex.EncodeToString(sum(ref))}})
	case strings.HasPrefix(arg, "content read"):
		ref, start, cont := f.reference(), 0, "next"
		if slices.Contains(in.args, "--cursor") {
			start, cont = 500, ""
		}
		data := slices.Clone(ref[start*2 : start*2+1000])
		if f.fault == "wrong samples" && start == 500 {
			data[0]++
		}
		return ok(map[string]any{"continuation": cont, "resource": map[string]any{"data": wav(data)}, "mp4_pcm": map[string]any{
			"range":            map[string]any{"start_sample": itoa(start), "end_sample": itoa(start + 500)},
			"range_pcm_sha256": hex.EncodeToString(sum(ref[start*2 : start*2+1000]))}})
	}
	f.t.Fatalf("unexpected command %q", in.args)
	return reply{}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (f *simulatedInstall) envelope(command string, data any, errText string, code int) reply {
	b, err := json.Marshal(map[string]any{"schema_version": 1, "command": command, "data": data, "error": errText})
	if err != nil {
		f.t.Fatal(err)
	}
	return reply{stdout: b, exit: code}
}

func runSimulatedInstall(t *testing.T, fault string) (*suite, *simulatedInstall) {
	t.Helper()
	tools, _ := fakeTools(t)
	f := &simulatedInstall{t: t, tools: tools, fault: fault}
	s := testSuite(t, f.run)
	f.s = s
	s.binary = filepath.Join(s.directory, "nemalo-under-test")
	sel, err := selectInstalled(tools, t.TempDir(), s.directory, s.env, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	f.sel = sel
	s.installed(sel)
	return s, f
}

func TestInstalledCasesUseSelectedToolsAndReferenceSamples(t *testing.T) {
	s, f := runSimulatedInstall(t, "")
	if s.err != nil {
		t.Fatal(s.err)
	}
	r := s.result.Installed
	names := []string{}
	for _, c := range r.Cases {
		names = append(names, c.Name)
		if c.Exit != c.ExpectedExit {
			t.Fatal("wrong exit", c)
		}
	}
	// Version runs for every tool except Defender, plus archive, encode and
	// reference runs.
	runs := 7
	if runtime.GOOS == "windows" {
		runs = 6
	}
	if r.Status != "passed" || !slices.Equal(names, installedCases) || len(r.Tools) != 4 || f.toolRuns != runs || len(r.ToolRuns) != runs {
		t.Fatal(r.Status, names, len(r.Tools), f.toolRuns)
	}
	for _, tool := range r.Tools {
		if tool.SHA256 == "" || tool.Bytes == 0 || (tool.Role != "defender" && tool.Version != "Tool 1.0") {
			t.Fatal("tool identity not recorded", tool)
		}
	}
	if r.PCM == nil || len(r.PCM.Ranges) != 2 || r.PCM.TotalSamples != "2000" || s.result.FixtureSHA256["eicar"] != eicarSHA256 {
		t.Fatal(r.PCM, s.result.FixtureSHA256)
	}
	if got, err := os.ReadFile(filepath.Join(s.directory, "installed-source", "eicar.txt")); err != nil || !bytes.Equal(got, eicar()) {
		t.Fatal("test file not retained", err)
	}
}

func TestInstalledFailuresStopLaterWork(t *testing.T) {
	for fault, last := range map[string]string{
		"scanner removes test file": "installed-scan-detection",
		"unchecked member":          "installed-archive-ingest",
		"other decoder":             "installed-decode-check",
		"wrong samples":             "installed-pcm-continuation",
	} {
		s, _ := runSimulatedInstall(t, fault)
		r := s.result.Installed
		if s.err == nil || r.Status != "failed" || len(r.Cases) == 0 || r.Cases[len(r.Cases)-1].Name != last {
			t.Errorf("%s: err=%v status=%s cases=%d", fault, s.err, r.Status, len(r.Cases))
		}
	}
}
