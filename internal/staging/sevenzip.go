package staging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// ErrNoArchiveTool means no 7-Zip executable is installed, so a RAR or 7z
// archive cannot be read; it is preserved, never guessed at.
var ErrNoArchiveTool = errors.New("7-Zip (7zz, 7z, or 7za) is not installed, so this archive cannot be read; it is preserved")

// SevenZip finds the installed 7-Zip; tests replace it.
var SevenZip = func() (string, error) {
	for _, name := range []string{"7zz", "7z", "7za"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrNoArchiveTool
}

const (
	maxArchiveSnapshot = 4 << 30
	maxListing         = 16 << 20
	// noPassword is passed so 7-Zip never waits for one; an encrypted
	// member then fails its check instead of prompting.
	noPassword = "-pnemalo-no-password"
)

// Extract reads a ZIP itself, and a RAR or 7z through the installed 7-Zip,
// with the same member accounting.
func Extract(ctx context.Context, src *os.Root, name string, dst *os.Root, supported func(string) bool, limits Limits) (Extraction, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".rar", ".7z":
		return Extract7z(ctx, src, name, dst, supported, limits)
	}
	return ExtractZip(ctx, src, name, dst, supported, limits)
}

// Supported reports an archive Extract reads: a ZIP itself, and a RAR or 7z
// through an installed 7-Zip, without which Extract returns ErrNoArchiveTool.
func Supported(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".rar", ".7z":
		return true
	}
	return false
}

// sevenEntry is one member of a 7-Zip technical listing.
type sevenEntry struct {
	raw, name                      string
	size, packed                   int64
	packedKnown                    bool
	sizeKnown                      bool
	volume, offset                 int64
	volumeKnown, offsetKnown       bool
	crc                            uint32
	crcKnown                       bool
	folder, link, encrypted        bool
	split, splitBefore, splitAfter bool
	splitFields                    uint8
	special, malformed, valid      bool
}

// Extract7z copies the archive to a private snapshot, lists it with
// 7-Zip, and extracts each eligible member by its exact listed name to
// standard output, into a fresh index-prefixed flat name. No member name is
// ever used as a path, and every extracted member must have its listed size
// and CRC. Members are classified as ExtractZip classifies them.
func Extract7z(ctx context.Context, src *os.Root, name string, dst *os.Root, supported func(string) bool, limits Limits) (Extraction, error) {
	x := Extraction{Members: []Member{}}
	exe, err := SevenZip()
	if err != nil {
		return x, ErrNoArchiveTool
	}
	info, err := src.Lstat(name)
	if err != nil {
		return x, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maxArchiveSnapshot {
		return x, errors.New("the archive must be a regular file of at most 4 GiB")
	}
	// 7-Zip needs an ordinary path, which a root opened through the
	// descriptor bridge does not have, so the snapshot goes to a private
	// temporary folder as check's snapshots do.
	tmp, err := os.MkdirTemp("", "nemalo-archive-")
	if err != nil {
		return x, err
	}
	defer os.RemoveAll(tmp)
	tmpRoot, err := safeio.OpenRoot(tmp)
	if err != nil {
		return x, err
	}
	defer tmpRoot.Close()
	snapshot := "archive" + strings.ToLower(filepath.Ext(name))
	if err := snapshotArchive(ctx, src, name, info, tmpRoot, snapshot, limits); err != nil {
		return x, err
	}
	// 7-Zip opens whatever format a file's bytes name. The type is fixed
	// from the signature and must match the extension, so a tar, cpio, disk
	// image, or ZIP renamed .rar or .7z is never read through 7-Zip.
	kind, err := archiveType(tmpRoot, snapshot, strings.ToLower(filepath.Ext(name)))
	if err != nil {
		return x, err
	}
	if limits.Coverage && kind != "7z" && (kind != "Rar5" || limits.child) {
		return x, errors.New("RAR container bytes are not completely accounted for; the archive is preserved")
	}
	var rar5Files []RAR5File
	if limits.Coverage && kind == "Rar5" {
		privateInfo, err := tmpRoot.Lstat(snapshot)
		if err != nil {
			return x, err
		}
		if privateInfo.Size() != info.Size() {
			return x, fmt.Errorf("%w: private snapshot size changed", ErrRAR5Coverage)
		}
		private, err := safeio.OpenRegular(tmpRoot, snapshot, privateInfo, os.O_RDONLY)
		if err != nil {
			return x, err
		}
		defer private.Close()
		rar5Files, err = RAR5Accounted(ctx, limits.proofReader(private), privateInfo.Size())
		if err != nil {
			return x, err
		}
	}
	if limits.child && (kind == "Rar" || kind == "Rar5") {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		out, code, err := limits.run(probeCtx, exe, nil, maxListing, "l", "-slt", "-t"+kind, noPassword, "--", filepath.Join(tmp, snapshot))
		keys, ok := strictArchiveBlock(out)
		_, hasError := keys["ERROR"]
		if err != nil || code != 0 || !ok || hasError || keys["Type"] != kind || keys["Physical Size"] != strconv.FormatInt(info.Size(), 10) ||
			keys["Multivolume"] != "-" || keys["Volumes"] != "1" || keys["Volume Index"] != "" || keys["Total Physical Size"] != "" ||
			(keys["Embedded Stub Size"] != "" && keys["Embedded Stub Size"] != "0") {
			return x, errors.New("the nested RAR does not establish one standalone container; volume or ambiguous evidence stays preserved")
		}
	}
	x, err = extractSnapshot(ctx, exe, kind, filepath.Join(tmp, snapshot), info.Size(), dst, supported, limits, rar5Files)
	if err == nil && kind == "7z" && (limits.tree == nil || limits.Coverage) {
		x.Whole = wholeArchive(ctx, exe, kind, filepath.Join(tmp, snapshot), "Physical Size", info.Size(), x.packed, limits)
	}
	return x, err
}

// wholeArchive lists the archive block and reports that it has no comment,
// warning, offset, or tail, and that key equals size. A RAR is never whole:
// RAR5 can carry service and unknown blocks 7-Zip skips, which no listed size
// accounts for. A 7z's physical size is computed from its header position,
// so bytes can hide between its packed streams and its header; it is whole
// only when its header size and its members' packed sizes add up to the file.
func wholeArchive(ctx context.Context, exe, kind, archive, key string, size, packed int64, limits Limits) bool {
	if kind != "7z" {
		return false
	}
	headerCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, code, err := limits.run(headerCtx, exe, nil, maxListing, "l", "-slt", "-t"+kind, noPassword, "--", archive)
	if err != nil || code != 0 || !wholeBlock(out, key, size) {
		return false
	}
	headers, err := strconv.ParseInt(blockValue(out, "Headers Size"), 10, 64)
	return err == nil && headers > 0 && headers+packed == size
}

// blockValue is one key of an archive block wholeBlock accepted.
func blockValue(out []byte, key string) string {
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if v, ok := strings.CutPrefix(line, key+" = "); ok {
			return v
		}
		if line == "----------" {
			break
		}
	}
	return ""
}

// wholeBlock reads the archive block line by line up to the member listing.
// Every line must be a key, each key once; a comment, an offset or tail, or
// any other line, such as a WARNINGS heading, refuses it.
func wholeBlock(out []byte, key string, size int64) bool {
	text := strings.ReplaceAll(string(out), "\r\n", "\n")
	start := strings.Index(text, "\n--\n")
	if start < 0 {
		return false
	}
	keys := map[string]string{}
	for _, line := range strings.Split(text[start+4:], "\n") {
		if line == "" {
			continue
		}
		if line == "----------" {
			return keys[key] == strconv.FormatInt(size, 10)
		}
		k, v, ok := strings.Cut(line, " = ")
		if _, seen := keys[k]; !ok || seen || k == "Comment" || k == "Offset" || k == "Tail Size" {
			return false
		}
		keys[k] = v
	}
	return false
}

// extractSnapshot lists a snapshot already typed by its signature and
// extracts its eligible members.
func extractSnapshot(ctx context.Context, exe, kind, archive string, size int64, dst *os.Root, supported func(string) bool, limits Limits, rar5Files []RAR5File) (Extraction, error) {
	x := Extraction{Members: []Member{}}
	rar5Coverage := limits.Coverage && kind == "Rar5"
	if rar5Coverage && rar5Files == nil {
		return x, fmt.Errorf("%w: missing private framing evidence", ErrRAR5Coverage)
	}
	listCtx, cancel := context.WithTimeout(ctx, 2*time.Minute+time.Duration(size>>27)*time.Minute)
	defer cancel()
	args := []string{"l", "-slt", "-ba", "-t" + kind, noPassword}
	if rar5Coverage {
		args = append(args, "-sccUTF-8")
	}
	args = append(args, "--", archive)
	listing, code, err := limits.run(listCtx, exe, nil, maxListing, args...)
	if listCtx.Err() != nil {
		return x, errors.New("7-Zip did not list the archive within its deadline")
	}
	if err != nil && code < 0 {
		return x, fmt.Errorf("7-Zip could not run: %w", err)
	}
	if code != 0 {
		return x, errors.New("7-Zip could not list the archive, which may be damaged, encrypted, or split into volumes; it is preserved")
	}
	entries, err := parseListing(listing)
	if err != nil {
		return x, err
	}
	if rar5Coverage && !rar5TableMatches(rar5Files, entries) {
		return x, fmt.Errorf("%w: maintained reader table does not match every physical file record", ErrRAR5Coverage)
	}
	x, err = extractListedSnapshot(ctx, exe, kind, archive, size, dst, supported, limits, entries)
	x.Whole = rar5Coverage
	return x, err
}

// Extract only the table already checked against these private snapshots.
func extractListedSnapshot(ctx context.Context, exe, kind, archive string, size int64, dst *os.Root, supported func(string) bool, limits Limits, entries []sevenEntry) (Extraction, error) {
	x := Extraction{Members: []Member{}}
	for _, e := range entries {
		if e.split && (kind == "Rar" || kind == "Rar5") {
			return x, errors.New("7-Zip lists an unresolved split member; the entire archive set is preserved")
		}
	}
	if len(entries) > limits.Members && limits.tree == nil {
		return x, fmt.Errorf("archive has %d members; the limit is %d", len(entries), limits.Members)
	}
	if err := limits.countMembers(len(entries)); err != nil {
		return x, err
	}
	if limits.tree != nil {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.name
		}
		limits.nestedBlocked = nestedVolumes(names)
	}
	count, folders := map[string]int{}, map[string]bool{}
	for _, e := range entries {
		x.packed += e.packed
		count[e.raw]++
		// A name also selects every member under a folder of that name.
		for dir := path.Dir(e.name); dir != "." && dir != "/" && !folders[dir]; dir = path.Dir(dir) {
			folders[dir] = true
		}
	}
	var total int64
	x.Complete = true
	// Each member is extracted by its own run, which decompresses a solid
	// block from its start, so the archive as a whole has one deadline and
	// a bounded number of runs.
	archiveCtx, cancelArchive := context.WithTimeout(ctx, archiveDeadline(size))
	defer cancelArchive()
	runs := 0
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return x, err
		}
		m := Member{Name: displayName(e.name), Bytes: e.size}
		m.Disposition, m.Detail = classifySeven(e, supported, limits)
		if limits.Coverage && e.folder && (!e.sizeKnown || !e.packedKnown) {
			m.Disposition, m.Detail = "directory_unknown_size", "directory bytes are not completely stated; preserved in the archive"
		}
		if m.Disposition == "eligible" && (count[e.raw] > 1 || folders[e.name]) {
			// Extraction by name would return every member of that name.
			m.Disposition, m.Detail = "duplicate_name", "another member has this name, or a folder of it; preserved in the archive"
		}
		if m.Disposition == "eligible" && (runs >= maxRuns || archiveCtx.Err() != nil) {
			m.Disposition, m.Detail = "failed", "the archive's extraction budget was spent; preserved in the archive"
			x.Complete = false
		}
		if m.Disposition == "eligible" {
			if err := limits.reserveMember(m.Bytes); err != nil {
				m.Disposition, m.Detail = "failed", err.Error()
				x.Complete = false
			} else if total+m.Bytes > limits.TotalBytes {
				m.Disposition, m.Detail = "too_large", "archive total exceeds the extraction budget"
				x.Complete = false
			} else {
				staged := fmt.Sprintf("%05d%s", i, strings.ToLower(path.Ext(e.name)))
				runs++
				if err := safeio.EnsureSpaceIn(dst, m.Bytes); err != nil {
					m.Disposition, m.Detail = "failed", err.Error()
					x.Complete = false
				} else if err := extractSeven(archiveCtx, exe, kind, archive, e, dst, staged, limits); err != nil {
					if ctx.Err() != nil {
						return x, ctx.Err()
					}
					_ = dst.Remove(staged)
					m.Disposition, m.Detail = "failed", err.Error()
					x.Complete = false
				} else {
					m.Disposition, m.Staged = "extracted", staged
					total += m.Bytes
				}
			}
		}
		x.Members = append(x.Members, m)
	}
	return x, nil
}

func snapshotArchive(ctx context.Context, src *os.Root, name string, info fs.FileInfo, dst *os.Root, snapshot string, limits Limits) error {
	if err := safeio.EnsureSpaceIn(dst, info.Size()); err != nil {
		return err
	}
	in, err := safeio.OpenRegular(src, name, info, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := dst.OpenFile(snapshot, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, copyErr := copyArchiveBytes(ctx, limits, name, info, in, out)
	closeErr := out.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	after, err := in.Stat()
	if err != nil || n != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return errors.Join(err, errors.New("the archive changed while it was copied"))
	}
	return nil
}

// parseListing reads a 7-Zip technical listing: blocks of "Key = Value"
// lines separated by blank lines. A line of any other form, as a member name
// holding a line break would produce, makes the listing unreadable.
func parseListing(out []byte) ([]sevenEntry, error) {
	text := strings.ReplaceAll(string(out), "\r\n", "\n")
	entries := []sevenEntry{}
	for _, block := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if strings.TrimSpace(block) == "" {
			continue
		}
		e := sevenEntry{}
		seen := map[string]bool{}
		for _, line := range strings.Split(block, "\n") {
			key, value, ok := strings.Cut(line, " = ")
			if !ok && strings.HasSuffix(line, " =") {
				key, value, ok = strings.TrimSuffix(line, " ="), "", true
			}
			if !ok || key == "" || seen[key] {
				return nil, errors.New("7-Zip listed a member it could not describe on one line; the archive is preserved")
			}
			seen[key] = true
			switch key {
			case "Path":
				e.raw, e.valid = value, true
			case "Folder":
				e.folder = value == "+"
			case "Size":
				if value != "" {
					n, err := strconv.ParseInt(value, 10, 64)
					if err != nil || n < 0 {
						e.malformed = true
					}
					e.size = n
					e.sizeKnown = err == nil && n >= 0
				}
			case "Packed Size":
				if n, err := strconv.ParseInt(value, 10, 64); err == nil && n >= 0 {
					e.packed, e.packedKnown = n, true
				}
			case "Volume Index", "Offset":
				// A standalone RAR prints an empty member Volume Index.
				// Empty is unknown; ZIP set checks still require known values.
				if value == "" {
					break
				}
				n, err := strconv.ParseInt(value, 10, 64)
				valid := err == nil && n >= 0
				e.malformed = e.malformed || !valid
				if key == "Volume Index" {
					e.volume, e.volumeKnown = n, valid
				} else {
					e.offset, e.offsetKnown = n, valid
				}
			case "Attributes":
				// Windows attribute letters, a Unix mode, or both: "A
				// -rw-r--r--" for 7z and ZIP, " -rw-r--r--" for RAR.
				// Only a field of letters alone is Windows attributes; a hex
				// word such as 0D800000 is not.
				for _, f := range strings.Fields(value) {
					if mode, ok := unixMode(f); ok {
						e.applyMode(mode)
					} else if strings.Trim(f, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == "" && strings.Contains(f, "D") {
						e.folder = true
					}
				}
			case "Mode":
				// tar and cpio listings state the Unix mode here.
				if mode, ok := unixMode(strings.TrimSpace(value)); ok {
					e.applyMode(mode)
				}
			case "Symbolic Link", "Hard Link", "Copy Link", "Link":
				if value != "" {
					e.link = true
				}
			case "CRC":
				if value != "" {
					n, err := strconv.ParseUint(value, 16, 32)
					if err != nil {
						e.malformed = true
					}
					e.crc, e.crcKnown = uint32(n), err == nil
				}
			case "Encrypted":
				e.encrypted = value == "+"
			case "Split Before", "Split After":
				e.split = e.split || value != "-"
				e.malformed = e.malformed || (value != "-" && value != "+")
				if key == "Split Before" {
					e.splitBefore, e.splitFields = value == "+", e.splitFields|1
				} else {
					e.splitAfter, e.splitFields = value == "+", e.splitFields|2
				}
			}
		}
		if !e.valid {
			return nil, errors.New("7-Zip listed a member without a path; the archive is preserved")
		}
		e.name = e.raw
		if runtime.GOOS == "windows" {
			e.name = strings.ReplaceAll(e.raw, `\`, "/")
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// unixMode reports the file type letter of a Unix mode string such as
// "-rw-r--r--" or "lrwxrwxrwx".
func unixMode(f string) (byte, bool) {
	if len(f) != 10 || !strings.ContainsRune("-dlcbps", rune(f[0])) {
		return 0, false
	}
	for _, c := range f[1:] {
		if !strings.ContainsRune("rwxsStT-", c) {
			return 0, false
		}
	}
	return f[0], true
}

func (e *sevenEntry) applyMode(mode byte) {
	switch mode {
	case 'l':
		e.link = true
	case 'd':
		e.folder = true
	case '-':
	default:
		e.special = true
	}
}

const maxRuns = 1000

// archiveDeadline bounds all extraction runs of one archive: ten minutes
// and a minute per 16 MiB of archive, at most an hour.
func archiveDeadline(size int64) time.Duration {
	return min(10*time.Minute+time.Duration(size>>24)*time.Minute, time.Hour)
}

// archiveType names the 7-Zip handler for the snapshot's signature, which
// must agree with the extension.
func archiveType(root *os.Root, name, ext string) (string, error) {
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 8)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch {
	case ext == ".7z" && bytes.HasPrefix(head, []byte("7z\xbc\xaf\x27\x1c")):
		return "7z", nil
	case ext == ".rar" && bytes.HasPrefix(head, []byte("Rar!\x1a\x07\x01\x00")):
		return "Rar5", nil
	case ext == ".rar" && bytes.HasPrefix(head, []byte("Rar!\x1a\x07\x00")):
		return "Rar", nil
	}
	return "", fmt.Errorf("the file is not a %s archive by its signature; it is preserved", strings.ToUpper(strings.TrimPrefix(ext, ".")))
}

func classifySeven(e sevenEntry, supported func(string) bool, limits Limits) (string, string) {
	name := e.name
	switch {
	case e.folder && (e.size != 0 || e.packed != 0):
		return "directory_with_data", "a directory entry that holds data; not extracted"
	case e.folder:
		return "directory", ""
	case e.malformed:
		return "unsafe_name", "the listing did not give a size or CRC 7-Zip could state; not extracted"
	case len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") || !fs.ValidPath(name):
		return "unsafe_name", "absolute, parent, drive, or malformed member path; not extracted"
	case e.link:
		return "link", "not followed"
	case e.special:
		return "special", "not a regular file"
	case e.encrypted:
		return "encrypted", "encrypted members are not extracted"
	case limits.tree != nil && limits.nestedBlocked[name]:
		return "nested_volume", "nested volume sets are not read; preserved in the outer archive"
	case nestedArchive[strings.ToLower(path.Ext(name))] && (limits.tree == nil || !Supported(name)):
		return "nested_archive", "preserved in the archive; not extracted"
	case !supported(name):
		return "unsupported_format", "preserved in the archive"
	case e.size > limits.MemberBytes:
		return "too_large", "member exceeds the per-file budget"
	case e.size == 0:
		return "empty", ""
	case !e.crcKnown:
		return "unsupported_method", "the archive states no CRC for this member; not extracted"
	case e.packedKnown && (e.packed == 0 || e.size/e.packed > int64(limits.Ratio)):
		// A solid archive does not state a packed size per member.
		return "suspicious_ratio", "declared expansion ratio is beyond the budget"
	}
	return "eligible", ""
}

// extractSeven writes one member, selected by its exact name with wildcards
// off, from 7-Zip's standard output, capped at its listed size plus one byte,
// and checks its size and CRC. 7-Zip names no file on disk.
func extractSeven(ctx context.Context, exe, kind, archive string, e sevenEntry, dst *os.Root, staged string, limits Limits) error {
	out, err := dst.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	sum := crc32.NewIEEE()
	w := &cappedWriter{w: io.MultiWriter(out, sum), limit: min(e.size, limits.MemberBytes) + 1}
	memberCtx, cancel := context.WithTimeout(ctx, 2*time.Minute+time.Duration(e.size>>26)*time.Minute)
	defer cancel()
	_, code, runErr := limits.run(memberCtx, exe, w, maxListing, "e", "-so", "-spd", "-ssc", "-bd", "-t"+kind, noPassword, "--", archive, e.raw)
	closeErr := out.Close()
	switch {
	case memberCtx.Err() != nil:
		return errors.New("7-Zip did not extract the member within its deadline")
	case closeErr != nil:
		return closeErr
	case code != 0:
		return fmt.Errorf("7-Zip reported an error extracting the member (exit %d)", code)
	case runErr != nil:
		return runErr
	case w.over:
		return errors.New("member is larger than listed")
	case w.n != e.size:
		return fmt.Errorf("member holds %d bytes, not the listed %d", w.n, e.size)
	case e.crcKnown && sum.Sum32() != e.crc:
		return fmt.Errorf("member CRC %08x differs from the listed %08x", sum.Sum32(), e.crc)
	}
	return nil
}

// cappedWriter accepts at most limit bytes and fails after that, which ends
// the extraction of a member that is larger than listed.
type cappedWriter struct {
	w        io.Writer
	n, limit int64
	over     bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.n+int64(len(p)) > c.limit {
		c.over = true
		return 0, errors.New("output is larger than its limit")
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Run with no input, so the reader can never wait for a password. Without
// out, standard output is returned within the requested listing allowance.
func runSevenLimit(ctx context.Context, exe string, out io.Writer, limit int64, args ...string) ([]byte, int, error) {
	return runSevenBounded(ctx, exe, out, limit, nil, args...)
}

func runSevenBounded(ctx context.Context, exe string, out io.Writer, limit int64, diagnostics *allowance, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.WaitDelay = 2 * time.Second
	var buf bytes.Buffer
	listing := &cappedWriter{w: &buf, limit: limit}
	if out == nil {
		cmd.Stdout = listing
	} else {
		cmd.Stdout = out
	}
	cmd.Stderr = io.Discard
	if diagnostics != nil {
		if out == nil {
			cmd.Stdout = budgetWriter{cmd.Stdout, diagnostics}
		}
		cmd.Stderr = budgetWriter{io.Discard, diagnostics}
	}
	err := cmd.Run()
	code := -1
	var exit *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exit):
		code = exit.ExitCode()
	}
	if out == nil && listing.over {
		return nil, -1, errors.New("7-Zip's listing exceeds its output budget")
	}
	return buf.Bytes(), code, err
}

// Volume sets. Split 7z and .zip.NNN streams are byte chunks. RAR and PKZIP
// segments have container relationships checked through the installed reader.
var (
	splitSeven = regexp.MustCompile(`(?i)^(.+)\.7z\.([0-9]{3})$`)
	splitZIP   = regexp.MustCompile(`(?i)^(.+)\.zip\.([0-9]{3,4})$`)
	rarPart    = regexp.MustCompile(`(?i)^(.+)\.part([0-9]{1,4})\.rar$`)
)

// VolumeOf reports the set a file name is a volume of, by base name and
// kind, and its number, or ok false for any other name.
func VolumeOf(name string) (set string, number int, ok bool) {
	base := filepath.Base(name)
	if m := splitZIP.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[2])
		return filepath.Join(filepath.Dir(name), m[1]) + ".zip", n, true
	}
	if m := splitSeven.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[2])
		return filepath.Join(filepath.Dir(name), m[1]) + ".7z", n, true
	}
	if m := rarPart.FindStringSubmatch(base); m != nil {
		n, _ := strconv.Atoi(m[2])
		return filepath.Join(filepath.Dir(name), m[1]) + ".rar", n, true
	}
	return "", 0, false
}

// ExtractSet reads a volume set whose names, root-relative and in volume
// order from 1, are all its volumes, with the same member accounting as one
// archive. A split 7z is joined in order into one snapshot. A RAR set is
// checked in isolated snapshots for matching roles, ordinals, and split-member
// continuity before an error-free assembled listing must account for all
// volumes; otherwise the set is refused and every volume preserved.
func ExtractSet(ctx context.Context, src *os.Root, names []string, dst *os.Root, supported func(string) bool, limits Limits) (Extraction, error) {
	x := Extraction{Members: []Member{}}
	if len(names) < 2 || len(names) > 1000 {
		return x, errors.New("a volume set has 2 to 1,000 volumes")
	}
	exe, err := SevenZip()
	if err != nil {
		return x, ErrNoArchiveTool
	}
	set, _, _ := VolumeOf(names[0])
	zipSet, _, segmentedZIP := ZIPSegmentOf(names[0])
	if segmentedZIP {
		set = zipSet
	}
	oldSet, _, old := OldRARVolumeOf(names[1])
	if old && !segmentedZIP {
		set = oldSet
	}
	split := strings.HasSuffix(strings.ToLower(set), ".7z")
	var total int64
	infos := make([]fs.FileInfo, len(names))
	for i, name := range names {
		gotSet, n, ok := VolumeOf(name)
		if segmentedZIP {
			gotSet, n, ok = ZIPSegmentOf(name)
			if i+1 == len(names) {
				gotSet, n, ok = name, i+1, strings.EqualFold(filepath.Ext(name), ".zip")
			}
		} else if old {
			gotSet, n, ok = OldRARVolumeOf(name)
			if i == 0 {
				gotSet, n, ok = name, 1, strings.EqualFold(filepath.Ext(name), ".rar")
			}
		}
		if !ok || !sameVolumeSet(gotSet, set) || n != i+1 {
			return x, errors.New("the volumes are not one set numbered from 1 without gaps; they are preserved")
		}
		info, err := src.Lstat(name)
		if err != nil {
			return x, err
		}
		if !info.Mode().IsRegular() || info.Size() < 1 || total+info.Size() > maxArchiveSnapshot {
			return x, errors.New("every volume must be a regular file, and the set at most 4 GiB")
		}
		infos[i], total = info, total+info.Size()
	}
	tmp, err := os.MkdirTemp("", "nemalo-archive-")
	if err != nil {
		return x, err
	}
	defer os.RemoveAll(tmp)
	tmpRoot, err := safeio.OpenRoot(tmp)
	if err != nil {
		return x, err
	}
	defer tmpRoot.Close()
	if segmentedZIP || strings.HasSuffix(strings.ToLower(set), ".zip") {
		return extractZIPSet(ctx, exe, src, names, infos, tmpRoot, tmp, dst, supported, limits, !segmentedZIP, total)
	}
	if split {
		if err := joinVolumes(ctx, src, names, infos, tmpRoot, "archive.7z", total, limits); err != nil {
			return x, err
		}
		kind, err := archiveType(tmpRoot, "archive.7z", ".7z")
		if err != nil {
			return x, err
		}
		// The archive must end where the joined volumes end: a stray extra
		// volume, or a tail left from another split, would be bytes 7-Zip
		// ignores while it was reported as read.
		archive := filepath.Join(tmp, "archive.7z")
		headerCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		out, code, err := limits.run(headerCtx, exe, nil, maxListing, "l", "-slt", "-t"+kind, noPassword, "--", archive)
		keys, ok := archiveBlock(out)
		if err != nil || code != 0 || !ok || keys["Physical Size"] != strconv.FormatInt(total, 10) {
			return x, errors.New("the joined volumes are not exactly one 7z archive; the set is preserved")
		}
		x, err = extractSnapshot(ctx, exe, kind, archive, total, dst, supported, limits, nil)
		if err == nil && (limits.tree == nil || limits.Coverage) {
			x.Whole = wholeArchive(ctx, exe, kind, archive, "Physical Size", total, x.packed, limits)
		}
		return x, err
	}
	kind, archive, err := snapshotRARSet(ctx, exe, src, names, infos, tmpRoot, tmp, old, total, limits)
	if err != nil {
		return x, err
	}
	return extractSnapshot(ctx, exe, kind, archive, total, dst, supported, limits, nil)
}

// archiveBlock reads the keys of the archive block of a listing without
// -ba, the block 7-Zip prints before its first member. An archive comment is
// printed raw inside that block and can hold any text, so nothing at or
// after a Comment key is read, and a key that appears twice refuses the
// block rather than letting either value stand.
func archiveBlock(out []byte) (map[string]string, bool) {
	text := strings.ReplaceAll(string(out), "\r\n", "\n")
	start := strings.Index(text, "\n--\n")
	if start < 0 {
		return nil, false
	}
	keys := map[string]string{}
	for _, line := range strings.Split(text[start+4:], "\n") {
		key, value, ok := strings.Cut(line, " = ")
		if !ok || key == "Comment" || strings.HasPrefix(line, "----------") {
			break
		}
		if _, seen := keys[key]; seen {
			return nil, false
		}
		keys[key] = value
	}
	return keys, true
}

// joinVolumes writes the volumes of a split archive in order into one
// snapshot, each checked unchanged after it is copied.
func joinVolumes(ctx context.Context, src *os.Root, names []string, infos []fs.FileInfo, dst *os.Root, snapshot string, total int64, limits Limits) error {
	if err := safeio.EnsureSpaceIn(dst, total); err != nil {
		return err
	}
	out, err := dst.OpenFile(snapshot, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	for i, name := range names {
		in, err := safeio.OpenRegular(src, name, infos[i], os.O_RDONLY)
		if err != nil {
			_ = out.Close()
			return err
		}
		n, copyErr := copyArchiveBytes(ctx, limits, name, infos[i], in, out)
		after, statErr := in.Stat()
		_ = in.Close()
		if copyErr != nil || statErr != nil || n != infos[i].Size() || after.Size() != infos[i].Size() || !after.ModTime().Equal(infos[i].ModTime()) {
			_ = out.Close()
			return errors.Join(copyErr, statErr, errors.New("a volume changed while it was copied"))
		}
	}
	return out.Close()
}
