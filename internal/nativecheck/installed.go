package nativecheck

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/scanner"
)

// InstalledTools names each installed executable by absolute path. Nothing is
// discovered from the caller's PATH (decision 0093).
type InstalledTools struct {
	ClamScan, Defender, SevenZip, FFmpeg, FFprobe string
}

type InstalledReport struct {
	Schema     string              `json:"schema"`
	Status     string              `json:"status"`
	Path       []string            `json:"path"`
	Tools      []ToolFact          `json:"tools"`
	Signatures *scanner.Signatures `json:"signatures,omitempty"`
	ToolRuns   []ToolRun           `json:"tool_runs"`
	Cases      []Command           `json:"cases"`
	PCM        *PCMComparison      `json:"pcm,omitempty"`
}

type ToolFact struct {
	Role    string `json:"role"`
	Path    string `json:"path"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
	Version string `json:"version,omitempty"`
}

type ToolRun struct {
	Name     string        `json:"name"`
	Role     string        `json:"role"`
	Args     []string      `json:"args"`
	Exit     int           `json:"exit"`
	Duration time.Duration `json:"duration_ns"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
}

// PCMComparison binds Nemalo's decoded result to the harness's own run of the
// same installed FFmpeg. It checks capture and selection, not the decoder.
type PCMComparison struct {
	SampleRate      int             `json:"sample_rate"`
	ReferenceBytes  int             `json:"reference_bytes"`
	ReferenceSHA256 string          `json:"reference_sha256"`
	TotalSamples    string          `json:"total_samples"`
	Ranges          []PCMRangeCheck `json:"ranges"`
}

type PCMRangeCheck struct {
	Start  string `json:"start_sample"`
	End    string `json:"end_sample"`
	SHA256 string `json:"sha256"`
}

const (
	installedDeadline        = 10 * time.Minute
	installedCommandDeadline = 3 * time.Minute
	maxToolHashBytes         = 1 << 30
	maxReferencePCM          = 1 << 20 // the fixture read allowance
	eicarSHA256              = "275a021bbfb6489e54d471899f7db9d1663fc695ec2fe2a2c4538aabf651fd0f"
)

var installedCases = []string{
	"installed-doctor", "installed-scan-clean", "installed-scan-detection", "installed-library-init",
	"missing-archive-reader", "installed-archive-ingest", "missing-decoder", "installed-decode-check",
	"installed-pcm-import-preview", "installed-pcm-import-apply", "installed-pcm-units", "installed-pcm-read", "installed-pcm-continuation",
}

type selectedTool struct {
	role, path string
	names      []string // PATH names Nemalo tries, in its order; empty for Defender
}

type installedSelection struct {
	tools []selectedTool
	path  []string
	env   []string
	goos  string
}

// eicar assembles the standard harmless antivirus test file at run time, so no
// repository file carries the signature. It is stored reversed.
func eicar() []byte {
	r := []byte(`*H+H$!ELIF-TSET-SURIVITNA-DRADNATS-RACIE$}7)CC7)^P(45XZP\4[PA@%P!O5X`)
	slices.Reverse(r)
	return r
}

func selectInstalled(t InstalledTools, checkout, workspace string, base []string, goos string) (*installedSelection, error) {
	sel := &installedSelection{goos: goos}
	if goos == "windows" {
		if t.ClamScan != "" || t.Defender == "" {
			return nil, errors.New("on Windows, select Microsoft Defender with --defender; Nemalo does not use ClamAV there")
		}
		sel.tools = append(sel.tools, selectedTool{role: "defender", path: t.Defender})
	} else {
		if t.Defender != "" || t.ClamScan == "" {
			return nil, errors.New("on Linux and macOS, select ClamAV with --clamscan; --defender is Windows-only")
		}
		sel.tools = append(sel.tools, selectedTool{role: "clamscan", path: t.ClamScan, names: []string{"clamscan"}})
	}
	sel.tools = append(sel.tools,
		selectedTool{role: "7-zip", path: t.SevenZip, names: []string{"7zz", "7z", "7za"}},
		selectedTool{role: "ffmpeg", path: t.FFmpeg, names: []string{"ffmpeg"}},
		selectedTool{role: "ffprobe", path: t.FFprobe, names: []string{"ffprobe"}})
	for i, tool := range sel.tools {
		if tool.path == "" || !filepath.IsAbs(tool.path) {
			return nil, fmt.Errorf("%s must be named by an absolute path", tool.role)
		}
		tool.path = filepath.Clean(tool.path)
		sel.tools[i].path = tool.path
		info, err := os.Stat(tool.path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", tool.role, err)
		}
		if !info.Mode().IsRegular() || goos != "windows" && info.Mode().Perm()&0o111 == 0 {
			return nil, fmt.Errorf("%s is not an executable regular file", tool.role)
		}
		for _, owned := range []string{checkout, workspace} {
			if within(owned, tool.path, goos) {
				return nil, fmt.Errorf("%s must be installed outside the checkout and validation workspace", tool.role)
			}
		}
		if tool.names != nil && !containsPath(sel.path, filepath.Dir(tool.path), goos) {
			sel.path = append(sel.path, filepath.Dir(tool.path))
		}
	}
	sel.env = replaceEnv(base, "PATH", strings.Join(sel.path, string(os.PathListSeparator)), goos)
	if goos == "windows" {
		key, root, err := defenderRoot(sel.tools[0].path)
		if err != nil {
			return nil, err
		}
		sel.env = replaceEnv(sel.env, key, root, goos)
	}
	pathext := envValue(sel.env, "PATHEXT", goos)
	for _, tool := range sel.tools {
		if tool.names == nil {
			continue
		}
		if got := firstOnPath(sel.path, tool.names, pathext, goos); !samePath(got, tool.path, goos) {
			return nil, fmt.Errorf("selected %s %q is not what Nemalo would run (%q)", tool.role, tool.path, got)
		}
	}
	return sel, nil
}

// defenderRoot maps a selected MpCmdRun.exe to the one environment root from
// which Nemalo's own scanner resolution finds it.
func defenderRoot(path string) (string, string, error) {
	dir, name := filepath.Split(path)
	if !strings.EqualFold(name, "MpCmdRun.exe") {
		return "", "", errors.New("--defender must name MpCmdRun.exe")
	}
	dir = filepath.Clean(dir)
	platform := filepath.Dir(dir)
	if strings.EqualFold(filepath.Base(platform), "Platform") {
		vendor := filepath.Dir(platform)
		if strings.EqualFold(filepath.Base(vendor), "Windows Defender") && strings.EqualFold(filepath.Base(filepath.Dir(vendor)), "Microsoft") {
			return "ProgramData", filepath.Dir(filepath.Dir(vendor)), nil
		}
	}
	if strings.EqualFold(filepath.Base(dir), "Windows Defender") {
		return "ProgramFiles", filepath.Dir(dir), nil
	}
	return "", "", errors.New("--defender is not in a Defender location Nemalo resolves")
}

// firstOnPath mirrors os/exec lookup for names tried in order, as a check that
// the selection is what Nemalo would actually run. It never executes anything.
func firstOnPath(dirs, names []string, pathext, goos string) string {
	exts := []string{""}
	if goos == "windows" {
		exts = nil
		for _, e := range strings.Split(strings.ToLower(pathext), ";") {
			if e != "" {
				exts = append(exts, e)
			}
		}
		if len(exts) == 0 {
			exts = []string{".com", ".exe", ".bat", ".cmd"}
		}
	}
	for _, name := range names {
		for _, dir := range dirs {
			for _, ext := range exts {
				candidate := filepath.Join(dir, name+ext)
				info, err := os.Stat(candidate)
				if err == nil && info.Mode().IsRegular() && (goos == "windows" || info.Mode().Perm()&0o111 != 0) {
					return candidate
				}
			}
		}
	}
	return ""
}

func samePath(a, b, goos string) bool {
	if goos == "windows" {
		return a != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return a != "" && filepath.Clean(a) == filepath.Clean(b)
}

func containsPath(list []string, p, goos string) bool {
	return slices.ContainsFunc(list, func(q string) bool { return samePath(q, p, goos) })
}

func within(root, p, goos string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, p)
	if goos == "windows" {
		rel, err = filepath.Rel(strings.ToLower(root), strings.ToLower(p))
	}
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func envKey(entry string) string { k, _, _ := strings.Cut(entry, "="); return k }

func sameKey(a, b, goos string) bool {
	if goos == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func replaceEnv(env []string, key, value, goos string) []string {
	out := []string{}
	for _, entry := range env {
		if !sameKey(envKey(entry), key, goos) {
			out = append(out, entry)
		}
	}
	return append(out, key+"="+value)
}

func envValue(env []string, key, goos string) string {
	for _, entry := range env {
		if k, v, _ := strings.Cut(entry, "="); sameKey(k, key, goos) {
			return v
		}
	}
	return ""
}

func (s *suite) installed(t *installedSelection) {
	r := &InstalledReport{Schema: "nemalo-installed-tools-check/1", Status: "failed", Path: t.path, Tools: []ToolFact{}, ToolRuns: []ToolRun{}, Cases: []Command{}}
	s.result.Installed = r
	s.result.Limitations = append(s.result.Limitations, "Installed-tool cases run only the named executables on owned synthetic fixtures. They show selected scanning, archive and decoding behavior, not detection coverage, malware resistance, every archive layout or codec, or decoder correctness.")
	scannerPath := t.tools[0].path
	hashed := int64(0)
	for _, tool := range t.tools {
		fact := ToolFact{Role: tool.role, Path: tool.path}
		fact.Bytes, fact.SHA256, hashed = s.hashTool(tool.path, hashed)
		r.Tools = append(r.Tools, fact)
	}
	toolPath := func(role string) string {
		for _, x := range t.tools {
			if x.role == role {
				return x.path
			}
		}
		return ""
	}
	versions := map[string][]string{"clamscan": {"--version"}, "7-zip": {}, "ffmpeg": {"-hide_banner", "-version"}, "ffprobe": {"-hide_banner", "-version"}}
	for i := range r.Tools {
		if args, ok := versions[r.Tools[i].Role]; ok {
			out := s.toolRun(r, t, r.Tools[i].Role+"-version", r.Tools[i].Role, r.Tools[i].Path, args...)
			r.Tools[i].Version = versionLine(out)
			s.require(r.Tools[i].Version != "", r.Tools[i].Role+" reported no version")
		}
	}
	for _, dir := range []string{"installed-source", "installed-intake", "installed-library", "installed-audio"} {
		s.mkdir(dir)
	}
	book, err := bookFixture("Installed tool acceptance. " + strings.Repeat("Synthetic passage. ", 200))
	s.fail(err)
	test := eicar()
	digest := sha256.Sum256(test)
	s.require(hex.EncodeToString(digest[:]) == eicarSHA256, "assembled antivirus test file is not the standard one")
	s.write("installed-source/book.epub", book)
	s.write("installed-source/eicar.txt", test)
	if s.err != nil {
		return
	}
	if s.result.FixtureSHA256 == nil {
		s.result.FixtureSHA256 = map[string]string{}
	}
	for _, name := range []string{"installed_book", "eicar"} {
		data := map[string][]byte{"installed_book": book, "eicar": test}[name]
		sum := sha256.Sum256(data)
		s.result.FixtureSHA256[name] = hex.EncodeToString(sum[:])
	}
	sourceBefore := s.snapshot("installed-source")
	call := func(name string, want int, dst any, args ...string) {
		s.callWith(&r.Cases, t.env, installedCommandDeadline, name, want, dst, args...)
	}
	missing := func(name string, want int, dst any, args ...string) {
		s.callWith(&r.Cases, s.env, installedCommandDeadline, name, want, dst, args...)
	}

	var doctor struct {
		Security     string              `json:"security_status"`
		Signatures   *scanner.Signatures `json:"signatures"`
		Capabilities []struct {
			Name      string `json:"name"`
			Available bool   `json:"available"`
		} `json:"capabilities"`
	}
	call("installed-doctor", 0, &doctor, "doctor")
	available := map[string]bool{}
	for _, c := range doctor.Capabilities {
		available[c.Name] = c.Available
	}
	scannerName := "clamscan"
	if t.goos == "windows" {
		scannerName = "MpCmdRun.exe"
	}
	s.require(doctor.Security == "not_scanned" && available[scannerName] && available["7z"] && available["ffmpeg"] && available["ffprobe"], "doctor did not report every selected tool")
	r.Signatures = doctor.Signatures
	if t.goos != "windows" {
		s.require(doctor.Signatures != nil && doctor.Signatures.Status != "unavailable", "doctor found no ClamAV signatures")
	}

	var report assessment.Report
	call("installed-scan-clean", 0, &report, "check", s.path("installed-source/book.epub"), "--scan")
	s.require(report.DetectedFormat == "epub" && report.Antivirus.Status == "no_detections_reported" && samePath(report.Antivirus.Tool, scannerPath, t.goos), "clean scan did not report coverage from the selected scanner")
	report = assessment.Report{}
	call("installed-scan-detection", 1, &report, "check", s.path("installed-source/eicar.txt"), "--scan")
	s.require(report.Antivirus.Status == "findings" && samePath(report.Antivirus.Tool, scannerPath, t.goos), "the selected scanner did not report the antivirus test file")
	s.require(bytes.Equal(s.read("installed-source/eicar.txt"), test), "the antivirus test file was changed or removed")
	s.unchanged("installed-source", sourceBefore)

	lib := s.path("installed-library")
	call("installed-library-init", 0, nil, "library", "init", lib)
	s.write("installed-audio/stage.epub", book) // Archive input only, removed once archived.
	archiveArgs := []string{"a", "-t7z", "-mx=1", "-bd", "--", s.path("installed-intake/book.7z"), s.path("installed-audio/stage.epub")}
	s.toolRun(r, t, "sevenzip-archive", "7-zip", toolPath("7-zip"), archiveArgs...)
	s.remove("installed-audio/stage.epub")
	archive := s.read("installed-intake/book.7z")
	s.require(bytes.HasPrefix(archive, []byte("7z\xbc\xaf\x27\x1c")), "7-Zip did not create a 7z archive")
	intakeBefore := s.snapshot("installed-intake")
	var ingest library.IngestResult
	missing("missing-archive-reader", 0, &ingest, "library", "ingest", lib, s.path("installed-intake"), "--apply", "--scan")
	s.require(ingest.Imported == 0 && ingest.NotImported == 1 && ingest.Failed == 0 && len(ingest.Items) == 1 && ingest.Items[0].Outcome == "not_imported" && strings.Contains(ingest.Items[0].Detail, "7-Zip"), "a missing archive reader did not preserve the archive")
	s.unchanged("installed-intake", intakeBefore)
	ingest = library.IngestResult{}
	call("installed-archive-ingest", 0, &ingest, "library", "ingest", lib, s.path("installed-intake"), "--apply", "--scan")
	s.require(ingest.Complete && ingest.Imported == 1 && ingest.Checked == 1 && ingest.Failed == 0, "installed 7-Zip and scanner did not import the member as checked")
	s.unchanged("installed-intake", intakeBefore)

	tone := s.path("installed-audio/tone.m4a")
	s.toolRun(r, t, "ffmpeg-encode", "ffmpeg", toolPath("ffmpeg"), "-nostdin", "-hide_banner", "-v", "error", "-n", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=2", "-ac", "1", "-c:a", "aac", "-b:a", "64k", "-f", "mp4", tone)
	audioBefore := s.snapshot("installed-audio")
	report = assessment.Report{}
	missing("missing-decoder", 1, &report, "check", tone, "--decode")
	s.require(report.Decode != nil && report.Decode.Status == "unavailable", "a missing decoder was not reported unavailable")
	report = assessment.Report{}
	call("installed-decode-check", 0, &report, "check", tone, "--decode")
	s.require(report.DetectedFormat == "m4a" && report.Decode != nil && report.Decode.Status == "passed" && report.Decode.DecodedSamples > 0 && samePath(report.Decode.Executable, toolPath("ffmpeg"), t.goos), "the selected FFmpeg did not decode the fixture")
	s.unchanged("installed-audio", audioBefore)

	var plan, held library.ImportResult
	call("installed-pcm-import-preview", 0, &plan, "library", "import", lib, tone)
	call("installed-pcm-import-apply", 0, &held, "library", "import", lib, tone, "--apply", "--plan", plan.Plan)
	s.require(held.Applied && held.AssetID != "" && held.Status == "review", "unscanned audio did not stay in review")
	if s.err != nil {
		return
	}
	s.pcm(r, t, lib, held.AssetID, tone, toolPath("ffmpeg"), call)
	s.unchanged("installed-audio", audioBefore)
	s.unchanged("installed-source", sourceBefore)
	if s.err == nil {
		names := make([]string, len(r.Cases))
		for i, c := range r.Cases {
			names[i] = c.Name
		}
		s.require(slices.Equal(names, installedCases), "installed-tool case sequence is incomplete")
	}
	if s.err == nil {
		r.Status = "passed"
	}
}

func (s *suite) pcm(r *InstalledReport, t *installedSelection, lib, asset, tone, ffmpeg string, call func(string, int, any, ...string)) {
	before := s.snapshot("installed-library")
	common := []string{lib, "--root", lib, "--asset", asset, "--decode"}
	var units, first, next content.Result
	call("installed-pcm-units", 0, &units, append([]string{"content", "units"}, append(common, "--representation", content.PCMRepresentation)...)...)
	if s.err != nil {
		return
	}
	pcm := units.MP4PCM
	s.require(pcm != nil && pcm.OutputRate > 0 && pcm.OutputChannels == 1 && pcm.FullPCMSHA256 != "", "decoded units lost their output description")
	if s.err != nil {
		return
	}
	s.toolRun(r, t, "ffmpeg-reference", "ffmpeg", ffmpeg, "-nostdin", "-hide_banner", "-v", "error", "-n", "-threads", "1", "-i", tone, "-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(pcm.OutputRate), "-c:a", "pcm_s16le", "-f", "s16le", s.path("installed-reference.pcm"))
	reference := s.read("installed-reference.pcm")
	if s.err != nil {
		return
	}
	sum := sha256.Sum256(reference)
	cmp := &PCMComparison{SampleRate: pcm.OutputRate, ReferenceBytes: len(reference), ReferenceSHA256: hex.EncodeToString(sum[:]), TotalSamples: pcm.TotalSamples, Ranges: []PCMRangeCheck{}}
	r.PCM = cmp
	s.require(len(reference) <= maxReferencePCM && len(reference) >= 4000 && len(reference)%2 == 0, "reference decode has an unexpected size")
	s.require(cmp.ReferenceSHA256 == pcm.FullPCMSHA256 && pcm.TotalSamples == strconv.Itoa(len(reference)/2), "full decoded output differs from the reference")
	call("installed-pcm-read", 0, &first, append([]string{"content", "read"}, append(common, "--representation", content.PCMRepresentation, "--pcm-sample", "0", "--max-bytes", "1044")...)...)
	s.pcmRange(cmp, first, reference, 0, 500)
	if s.err != nil {
		return
	}
	s.require(first.Continuation != "", "decoded read returned no continuation")
	call("installed-pcm-continuation", 0, &next, append([]string{"content", "read"}, append(common, "--cursor", first.Continuation, "--max-bytes", "1044")...)...)
	s.pcmRange(cmp, next, reference, 500, 1000)
	s.unchanged("installed-library", before)
}

func (s *suite) pcmRange(cmp *PCMComparison, got content.Result, reference []byte, start, end int) {
	if s.err != nil {
		return
	}
	if got.MP4PCM == nil || got.MP4PCM.Range == nil || got.Resource == nil {
		s.fail(errors.New("decoded read returned no range"))
		return
	}
	rng := got.MP4PCM.Range
	samples, err := wavData(got.Resource.Data)
	want := reference[start*2 : end*2]
	digest := sha256.Sum256(want)
	cmp.Ranges = append(cmp.Ranges, PCMRangeCheck{Start: rng.StartSample, End: rng.EndSample, SHA256: got.MP4PCM.RangePCMSHA256})
	s.require(err == nil && rng.StartSample == strconv.Itoa(start) && rng.EndSample == strconv.Itoa(end) && bytes.Equal(samples, want) && got.MP4PCM.RangePCMSHA256 == hex.EncodeToString(digest[:]), fmt.Sprintf("decoded samples %d-%d differ from the reference", start, end))
}

// wavData returns the data chunk of a little-endian RIFF WAVE resource.
func wavData(b []byte) ([]byte, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("not a WAVE resource")
	}
	for off := 12; off+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		if size < 0 || off+8+size > len(b) {
			return nil, errors.New("truncated WAVE chunk")
		}
		if string(b[off:off+4]) == "data" {
			return b[off+8 : off+8+size], nil
		}
		off += 8 + size + size%2
	}
	return nil, errors.New("WAVE resource has no data chunk")
}

// toolRun invokes a selected tool directly with an argument array, for
// fixture generation, the reference decode and version lines.
func (s *suite) toolRun(r *InstalledReport, t *installedSelection, name, role, exe string, args ...string) []byte {
	if !s.guard() {
		return nil
	}
	started := time.Now()
	in, err := s.checkedInvocation(args)
	if err != nil {
		s.fail(err)
		return nil
	}
	in.binary, in.env, in.deadline = exe, t.env, installedCommandDeadline
	res := s.run(s.ctx, in)
	s.lastOutput = &CommandOutput{Name: name, Stdout: string(res.stdout[:min(len(res.stdout), 16<<10)]), Stderr: string(res.stderr[:min(len(res.stderr), 4<<10)]), StdoutTruncated: len(res.stdout) > 16<<10, StderrTruncated: len(res.stderr) > 4<<10}
	s.commandCount++
	stdout, stderr := fmt.Sprintf("%02d.stdout.txt", s.commandCount), fmt.Sprintf("%02d.stderr.txt", s.commandCount)
	r.ToolRuns = append(r.ToolRuns, ToolRun{Name: name, Role: role, Args: args, Exit: res.exit, Duration: time.Since(started), Stdout: stdout, Stderr: stderr})
	s.outputBytes += len(res.stdout) + len(res.stderr)
	if s.outputBytes > 8<<20 {
		s.fail(errors.New("native acceptance diagnostic budget exceeded"))
		return nil
	}
	s.fail(errors.Join(s.root.WriteFile(stdout, res.stdout, 0600), s.root.WriteFile(stderr, res.stderr, 0600)))
	if s.err == nil && res.err != nil {
		s.fail(fmt.Errorf("%s: %w", name, res.err))
	}
	if s.err == nil && res.exit != 0 {
		s.fail(fmt.Errorf("%s: exit %d; retained diagnostics %s", name, res.exit, stderr))
	}
	if s.err != nil {
		return nil
	}
	return res.stdout
}

func (s *suite) hashTool(path string, used int64) (int64, string, int64) {
	if !s.guard() {
		return 0, "", used
	}
	f, err := os.Open(path)
	if err != nil {
		s.fail(err)
		return 0, "", used
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || used+info.Size() > maxToolHashBytes {
		s.fail(errors.Join(err, errors.New("selected tool is not a regular file within the hash allowance")))
		return 0, "", used
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, info.Size()+1))
	if err == nil && n != info.Size() {
		err = errors.New("selected tool changed while hashing")
	}
	s.fail(err)
	return n, hex.EncodeToString(h.Sum(nil)), used + n
}

// versionLine is the first nonblank output line, bounded and without control
// characters; the report is encoded through textsafe as well.
func versionLine(out []byte) string {
	for _, line := range strings.Split(strings.ToValidUTF8(string(out), "?"), "\n") {
		line = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, line))
		if line != "" {
			if len(line) > 200 {
				line = strings.ToValidUTF8(line[:200], "")
			}
			return line
		}
	}
	return ""
}
