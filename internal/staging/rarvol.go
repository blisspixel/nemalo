package staging

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/safeio"
)

var oldRAR = regexp.MustCompile(`(?i)^(.+)\.([r-z])([0-9]{2})$`)

// OldRARVolumeOf recognizes an old-style continuation, not a standalone .rar.
// Its set is the expected first .rar; its ordinal starts at two for .r00.
func OldRARVolumeOf(name string) (set string, number int, ok bool) {
	m := oldRAR.FindStringSubmatch(filepath.Base(name))
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.Atoi(m[3])
	return filepath.Join(filepath.Dir(name), m[1]) + ".rar", 2 + int(strings.ToLower(m[2])[0]-'r')*100 + n, true
}

func sameVolumeSet(a, b string) bool {
	return filepath.Dir(a) == filepath.Dir(b) && strings.EqualFold(filepath.Base(a), filepath.Base(b))
}

func rarSnapshotName(old bool, index int) string {
	if !old {
		return fmt.Sprintf("archive.part%04d.rar", index+1)
	}
	if index == 0 {
		return "archive.rar"
	}
	return fmt.Sprintf("archive.%c%02d", 'r'+(index-1)/100, (index-1)%100)
}

// snapshotRARSet probes each snapshot with no siblings before assembling it.
// Reader-declared roles and ordinals are evidence, not a cryptographic set ID.
func snapshotRARSet(ctx context.Context, exe string, src *os.Root, names []string, infos []fs.FileInfo, dst *os.Root, dir string, old bool, total int64, limits Limits, proof *rar5SetBinding) (string, string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	remaining := int64(maxListing)
	kind := ""
	var previous *sevenEntry
	listed := 0
	for i, name := range names {
		sub := fmt.Sprintf("volume-%04d", i)
		if err := dst.Mkdir(sub, 0o700); err != nil {
			return "", "", err
		}
		snapshot := filepath.Join(sub, rarSnapshotName(old, i))
		if err := snapshotArchive(probeCtx, src, name, infos[i], dst, snapshot, limits); err != nil {
			return "", "", err
		}
		got, err := archiveType(dst, snapshot, ".rar")
		if err != nil {
			return "", "", err
		}
		if i == 0 {
			kind = got
		}
		if got != kind {
			return "", "", errors.New("RAR volumes disagree on their format; the set is preserved")
		}
		if proof != nil && kind != "Rar5" {
			return "", "", fmt.Errorf("%w: only RAR5 set framing is supported", ErrRAR5Coverage)
		}
		args := []string{"l", "-slt", "-t" + kind, noPassword}
		if proof != nil {
			args = append(args, "-sccUTF-8")
		}
		args = append(args, "--", filepath.Join(dir, snapshot))
		out, code, err := limits.run(probeCtx, exe, nil, remaining, args...)
		remaining -= int64(len(out))
		next := ""
		if i+1 < len(names) {
			next = rarSnapshotName(old, i+1)
		}
		probe, splitDiagnostic := rarProbeOutput(out, kind, i)
		if probeCtx.Err() != nil {
			return "", "", probeCtx.Err()
		}
		if err != nil && code < 0 {
			return "", "", err
		}
		if (err != nil && code != 2) || !rarVolumeHeader(probe, code, kind, infos[i].Size(), i, old, next, splitDiagnostic) {
			return "", "", fmt.Errorf("RAR volume %d has inconclusive or incompatible format, role, ordinal, size, or naming evidence; the set is preserved", i+1)
		}
		entries, err := rarProbeMembers(probe)
		listed += len(entries)
		if err != nil || listed > limits.memberLimit()+len(names)-1 || !rarBoundary(entries, previous, i+1 < len(names)) {
			return "", "", fmt.Errorf("RAR volume %d has inconclusive or incompatible split-member continuity; the set is preserved", i+1)
		}
		if proof != nil {
			info, err := dst.Lstat(snapshot)
			if err != nil {
				return "", "", err
			}
			if info.Size() != infos[i].Size() {
				return "", "", fmt.Errorf("%w: private volume size changed", ErrRAR5Coverage)
			}
			file, err := safeio.OpenRegular(dst, snapshot, info, os.O_RDONLY)
			if err != nil {
				return "", "", err
			}
			volume, proofErr := RAR5VolumeAccounted(probeCtx, limits.proofReader(file), info.Size())
			closeErr := file.Close()
			if proofErr != nil {
				return "", "", proofErr
			}
			if closeErr != nil {
				return "", "", closeErr
			}
			if !rar5VolumeTableMatches(probeCtx, volume, entries) {
				if probeCtx.Err() != nil {
					return "", "", probeCtx.Err()
				}
				return "", "", fmt.Errorf("%w: isolated table disagrees with physical volume", ErrRAR5Coverage)
			}
			if err := proof.add(probeCtx, volume, i, len(names), limits.memberLimit()); err != nil {
				return "", "", err
			}
		}
		last := entries[len(entries)-1]
		previous = &last
	}
	// All probes finished before any sibling becomes visible to the reader.
	for i := range names {
		if err := probeCtx.Err(); err != nil {
			return "", "", err
		}
		name := rarSnapshotName(old, i)
		if err := dst.Rename(filepath.Join(fmt.Sprintf("volume-%04d", i), name), name); err != nil {
			return "", "", err
		}
	}
	archive := filepath.Join(dir, rarSnapshotName(old, 0))
	args := []string{"l", "-slt", "-t" + kind, noPassword}
	if proof != nil {
		args = append(args, "-sccUTF-8")
	}
	args = append(args, "--", archive)
	out, code, err := limits.run(probeCtx, exe, nil, remaining, args...)
	keys, ok := strictArchiveBlock(out)
	if probeCtx.Err() != nil {
		return "", "", probeCtx.Err()
	}
	_, hasError := keys["ERROR"]
	if err != nil || code != 0 || !ok || hasError {
		return "", "", errors.New("7-Zip could not read the complete RAR volume set; it is preserved")
	}
	volumes, _ := strconv.Atoi(keys["Volumes"])
	if keys["Multivolume"] != "+" || volumes != len(names) {
		return "", "", fmt.Errorf("7-Zip reads this set as %d volume(s), not the %d found; it is preserved", volumes, len(names))
	}
	if keys["Type"] != kind || keys["Volume Index"] != "0" || keys["Physical Size"] != strconv.FormatInt(infos[0].Size(), 10) || keys["Total Physical Size"] != strconv.FormatInt(total, 10) {
		return "", "", errors.New("7-Zip cannot account for every RAR volume's size and order; the set is preserved")
	}
	if proof != nil {
		entries, err := rarListedMembers(out, false)
		if err != nil {
			return "", "", err
		}
		if !proof.matchesAssembled(probeCtx, entries) {
			if probeCtx.Err() != nil {
				return "", "", probeCtx.Err()
			}
			return "", "", fmt.Errorf("%w: assembled table disagrees with complete physical records", ErrRAR5Coverage)
		}
		proof.entries = entries
	}
	return kind, archive, nil
}

func rarVolumeHeader(out []byte, code int, kind string, size int64, index int, old bool, next string, splitDiagnostic bool) bool {
	keys, ok := strictArchiveBlock(out)
	if !ok || keys["Type"] != kind || keys["Physical Size"] != strconv.FormatInt(size, 10) || keys["Multivolume"] != "+" || keys["Volume Index"] != strconv.Itoa(index) || keys["Volumes"] != "1" {
		return false
	}
	if _, has := keys["Total Physical Size"]; has {
		return false
	}
	if kind == "Rar" {
		flags := strings.Fields(keys["Characteristics"])
		if !slices.Contains(flags, "Volume") || slices.Contains(flags, "FirstVolume") != (index == 0) || slices.Contains(flags, "NewVolName") == old {
			return false
		}
	}
	problem, hasError := keys["ERROR"]
	missingNext := hasError && next != "" && problem == "Missing volume : "+next
	return (code == 0 && !hasError && !splitDiagnostic) || (code == 2 && (!hasError || missingNext) && (missingNext || splitDiagnostic))
}

// RAR5's reader marks an isolated Split Before member as a header error.
// Remove only that exact diagnostic for the identity probe; continuity and
// the subsequent error-free assembled listing must both pass independently.
func rarProbeOutput(out []byte, kind string, index int) ([]byte, bool) {
	text := strings.ReplaceAll(string(out), "\r\n", "\n")
	const diagnostic = "\nERRORS:\nHeaders Error\n"
	if kind != "Rar5" || index == 0 || strings.Count(text, diagnostic) != 1 {
		return []byte(text), false
	}
	return []byte(strings.Replace(text, diagnostic, "\n", 1)), true
}

func rarProbeMembers(out []byte) ([]sevenEntry, error) {
	return rarListedMembers(out, true)
}

// Only isolated identity probes may remove their narrowly allowed error summary.
// An assembled table must parse in full, with no diagnostic exception.
func rarListedMembers(out []byte, isolated bool) ([]sevenEntry, error) {
	text := strings.ReplaceAll(string(out), "\r\n", "\n")
	_, body, ok := strings.Cut(text, "\n----------\n")
	if !ok {
		return nil, errors.New("no RAR member listing")
	}
	if before, summary, has := strings.Cut(body, "\n\nErrors: "); isolated && has {
		if strings.TrimSpace(summary) != "1" && strings.TrimSpace(summary) != "2" {
			return nil, errors.New("ambiguous RAR probe summary")
		}
		body = before
	}
	return parseListing([]byte(body))
}

// Without a cross-volume member there is no shared container identity to
// compare. Preserve such a boundary rather than joining files by ordinal.
func rarBoundary(entries []sevenEntry, previous *sevenEntry, more bool) bool {
	if len(entries) == 0 {
		return false
	}
	first, last := entries[0], entries[len(entries)-1]
	if previous == nil {
		if first.splitBefore {
			return false
		}
	} else if !first.splitBefore || first.raw != previous.raw || first.size != previous.size || first.size <= 0 {
		return false
	}
	for i, e := range entries {
		if e.malformed || e.splitFields != 3 || (i > 0 && e.splitBefore) || (i+1 < len(entries) && e.splitAfter) {
			return false
		}
	}
	return last.splitAfter == more
}

// strictArchiveBlock requires an unambiguous, terminated archive block. A comment
// can contain forged keys, so even a comment after otherwise valid keys refuses.
func strictArchiveBlock(out []byte) (map[string]string, bool) {
	text := strings.ReplaceAll(string(out), "\r\n", "\n")
	start := strings.Index(text, "\n--\n")
	if start < 0 {
		return nil, false
	}
	keys := map[string]string{}
	for _, line := range strings.Split(text[start+4:], "\n") {
		if line == "" {
			continue
		}
		if line == "----------" {
			return keys, true
		}
		key, value, ok := strings.Cut(line, " = ")
		if _, seen := keys[key]; !ok || key == "" || seen || key == "Comment" || key == "Offset" || key == "Tail Size" || key == "Warnings" || key == "Errors" {
			return nil, false
		}
		keys[key] = value
	}
	return nil, false
}
