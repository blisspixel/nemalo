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
	raw, name                 string
	size, packed              int64
	packedKnown               bool
	crc                       uint32
	crcKnown                  bool
	folder, link, encrypted   bool
	special, malformed, valid bool
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
	if err := snapshotArchive(ctx, src, name, info, tmpRoot, snapshot); err != nil {
		return x, err
	}
	archive := filepath.Join(tmp, snapshot)
	listCtx, cancel := context.WithTimeout(ctx, 2*time.Minute+time.Duration(info.Size()>>27)*time.Minute)
	defer cancel()
	listing, code, err := runSeven(listCtx, exe, nil, "l", "-slt", "-ba", noPassword, "--", archive)
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
	if len(entries) > limits.Members {
		return x, fmt.Errorf("archive has %d members; the limit is %d", len(entries), limits.Members)
	}
	count := map[string]int{}
	for _, e := range entries {
		count[e.raw]++
	}
	var total int64
	x.Complete = true
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return x, err
		}
		m := Member{Name: displayName(e.name), Bytes: e.size}
		m.Disposition, m.Detail = classifySeven(e, supported, limits)
		if m.Disposition == "eligible" && count[e.raw] > 1 {
			// Extraction by name would return every member of that name.
			m.Disposition, m.Detail = "duplicate_name", "another member has this exact name; preserved in the archive"
		}
		if m.Disposition == "eligible" {
			if total+m.Bytes > limits.TotalBytes {
				m.Disposition, m.Detail = "too_large", "archive total exceeds the extraction budget"
				x.Complete = false
			} else {
				staged := fmt.Sprintf("%05d%s", i, strings.ToLower(path.Ext(e.name)))
				if err := safeio.EnsureSpaceIn(dst, m.Bytes); err != nil {
					m.Disposition, m.Detail = "failed", err.Error()
					x.Complete = false
				} else if err := extractSeven(ctx, exe, archive, e, dst, staged, limits.MemberBytes); err != nil {
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

func snapshotArchive(ctx context.Context, src *os.Root, name string, info fs.FileInfo, dst *os.Root, snapshot string) error {
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
	n, copyErr := io.Copy(out, io.LimitReader(contextReader{ctx, in}, info.Size()+1))
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
				}
			case "Packed Size":
				if n, err := strconv.ParseInt(value, 10, 64); err == nil && n >= 0 {
					e.packed, e.packedKnown = n, true
				}
			case "Attributes":
				// Windows attribute letters, a Unix mode, or both: "A
				// -rw-r--r--" for 7z and ZIP, " -rw-r--r--" for RAR.
				for _, f := range strings.Fields(value) {
					if mode, ok := unixMode(f); ok {
						switch mode {
						case 'l':
							e.link = true
						case 'd':
							e.folder = true
						case '-':
						default:
							e.special = true
						}
					} else if strings.Contains(f, "D") {
						e.folder = true
					}
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

func classifySeven(e sevenEntry, supported func(string) bool, limits Limits) (string, string) {
	name := e.name
	switch {
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
	case nestedArchive[strings.ToLower(path.Ext(name))]:
		return "nested_archive", "preserved in the archive; not extracted"
	case !supported(name):
		return "unsupported_format", "preserved in the archive"
	case e.size > limits.MemberBytes:
		return "too_large", "member exceeds the per-file budget"
	case e.size == 0:
		return "empty", ""
	case e.packedKnown && (e.packed == 0 || e.size/e.packed > int64(limits.Ratio)):
		// A solid archive does not state a packed size per member.
		return "suspicious_ratio", "declared expansion ratio is beyond the budget"
	}
	return "eligible", ""
}

// extractSeven writes one member, selected by its exact name with wildcards
// off, from 7-Zip's standard output, capped at its listed size plus one byte,
// and checks its size and CRC. 7-Zip names no file on disk.
func extractSeven(ctx context.Context, exe, archive string, e sevenEntry, dst *os.Root, staged string, limit int64) error {
	out, err := dst.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	sum := crc32.NewIEEE()
	w := &cappedWriter{w: io.MultiWriter(out, sum), limit: min(e.size, limit) + 1}
	memberCtx, cancel := context.WithTimeout(ctx, 2*time.Minute+time.Duration(e.size>>26)*time.Minute)
	defer cancel()
	_, code, runErr := runSeven(memberCtx, exe, w, "e", "-so", "-spd", "-ssc", "-bd", noPassword, "--", archive, e.raw)
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

// runSeven runs 7-Zip with no input, so it can never wait for a password.
// Without out, standard output is returned, up to 16 MiB.
func runSeven(ctx context.Context, exe string, out io.Writer, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.WaitDelay = 2 * time.Second
	var buf bytes.Buffer
	listing := &cappedWriter{w: &buf, limit: maxListing}
	if out == nil {
		cmd.Stdout = listing
	} else {
		cmd.Stdout = out
	}
	cmd.Stderr = io.Discard
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
		return nil, code, errors.New("7-Zip's listing exceeds 16 MiB")
	}
	return buf.Bytes(), code, err
}
