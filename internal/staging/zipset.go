package staging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/blisspixel/nemalo/internal/safeio"
)

var zipSegment = regexp.MustCompile(`(?i)^(.+)\.z([0-9]{2,4})$`)

var ErrZIPCoverage = errors.New("ZIP byte coverage is not established")

// ZIPSegmentOf recognizes a PKZIP segment. The set names its required terminal
// .zip sibling, not its first segment. A plain .zip is not itself a segment.
func ZIPSegmentOf(name string) (set string, number int, ok bool) {
	m := zipSegment.FindStringSubmatch(filepath.Base(name))
	if m == nil {
		return "", 0, false
	}
	n, _ := strconv.Atoi(m[2])
	return filepath.Join(filepath.Dir(name), m[1]) + ".zip", n, true
}

func extractZIPSet(ctx context.Context, exe string, src *os.Root, names []string, infos []fs.FileInfo, snapshots *os.Root, dir string, dst *os.Root, supported func(string) bool, limits Limits, stream bool, total int64) (Extraction, error) {
	x := Extraction{Members: []Member{}}
	prepCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	remaining := int64(maxListing)
	if stream {
		if err := joinVolumes(prepCtx, src, names, infos, snapshots, "archive.zip", total); err != nil {
			return x, err
		}
		if !zipSignature(snapshots, "archive.zip", false) {
			return x, errors.New("the joined volumes are not an ordinary ZIP stream by signature; the set is preserved")
		}
		if err := ordinaryZIP(prepCtx, exe, filepath.Join(dir, "archive.zip"), total, remaining, limits); err != nil {
			return x, err
		}
		if limits.ZIPCoverage {
			info, err := snapshots.Lstat("archive.zip")
			if err != nil {
				return x, err
			}
			f, err := safeio.OpenRegular(snapshots, "archive.zip", info, os.O_RDONLY)
			if err != nil {
				return x, err
			}
			accounted, why := ZIPAccounted(prepCtx, f, total)
			f.Close()
			if !accounted {
				return x, fmt.Errorf("%w: %s", ErrZIPCoverage, why)
			}
		}
		cancel()
		x, err := ExtractZip(ctx, snapshots, "archive.zip", dst, supported, limits)
		x.Whole = limits.ZIPCoverage && err == nil
		return x, err
	}
	privateNames := make([]string, len(names))
	for i, name := range names {
		privateNames[i] = fmt.Sprintf("archive.z%02d", i+1)
		if i+1 == len(names) {
			privateNames[i] = "archive.zip"
		}
		if err := snapshotArchive(prepCtx, src, name, infos[i], snapshots, privateNames[i]); err != nil {
			return x, err
		}
	}
	if !zipSignature(snapshots, privateNames[0], true) {
		return x, errors.New("the first ZIP segment lacks the split marker; the set is preserved")
	}
	var table []byte
	var entries []sevenEntry
	for i, name := range privateNames {
		out, code, err := limits.run(prepCtx, exe, nil, remaining, "l", "-slt", "-tzip", noPassword, "--", filepath.Join(dir, name))
		remaining -= int64(len(out))
		keys, ok := strictArchiveBlock(out)
		_, hasError := keys["ERROR"]
		if err != nil || code != 0 || !ok || hasError || keys["Type"] != "zip" ||
			keys["Physical Size"] != strconv.FormatInt(infos[i].Size(), 10) ||
			keys["Total Physical Size"] != strconv.FormatInt(total, 10) || keys["Multivolume"] != "+" ||
			keys["Volume Index"] != strconv.Itoa(i) || keys["Volumes"] != strconv.Itoa(len(names)) ||
			keys["Embedded Stub Size"] != "4" {
			return x, fmt.Errorf("ZIP volume %d has inconclusive or incompatible reader, size, count, ordinal, or split-marker evidence; the set is preserved", i+1)
		}
		_, body, ok := bytes.Cut(bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n")), []byte("\n----------\n"))
		if !ok {
			return x, errors.New("the ZIP reader did not list the complete member table; the set is preserved")
		}
		body = bytes.TrimSpace(body)
		if i == 0 {
			var err error
			entries, err = parseListing(body)
			if err != nil || len(entries) > limits.memberLimit() || !zipMemberBounds(entries, infos, total) {
				return x, errors.New("the ZIP member table has inconclusive or out-of-range disk, offset, or size evidence; the set is preserved")
			}
			table = body
		} else if !bytes.Equal(body, table) {
			return x, errors.New("the ZIP segments disagree on the member table; the set is preserved")
		}
	}
	if limits.ZIPCoverage {
		if accounted, why := zipSegmentsAccounted(prepCtx, snapshots, privateNames, entries); !accounted {
			return x, fmt.Errorf("%w: %s", ErrZIPCoverage, why)
		}
		cancel()
		x, err := extractListedSnapshot(ctx, exe, "zip", filepath.Join(dir, privateNames[0]), total, dst, supported, limits, entries)
		x.Whole = err == nil
		return x, err
	}
	cancel()
	return extractSnapshot(ctx, exe, "zip", filepath.Join(dir, privateNames[0]), total, dst, supported, limits)
}

// ordinaryZIP supplies the maintained reader's extent and volume facts before
// the same private bytes are handed to the standard ZIP extractor.
func ordinaryZIP(ctx context.Context, exe, archive string, size, outputLimit int64, limits Limits) error {
	out, code, err := limits.run(ctx, exe, nil, outputLimit, "l", "-slt", "-tzip", noPassword, "--", archive)
	keys, ok := strictArchiveBlock(out)
	_, hasError := keys["ERROR"]
	if err != nil || code != 0 || !ok || hasError || keys["Type"] != "zip" || keys["Physical Size"] != strconv.FormatInt(size, 10) ||
		(keys["Embedded Stub Size"] != "" && keys["Embedded Stub Size"] != "0") ||
		(keys["Multivolume"] != "" && keys["Multivolume"] != "-") ||
		(keys["Volumes"] != "" && keys["Volumes"] != "1") ||
		(keys["Volume Index"] != "" && keys["Volume Index"] != "0") || keys["Total Physical Size"] != "" {
		return errors.New("the private bytes do not establish exactly one ordinary ZIP without a stub, comment, warning, error, or tail; original sources are preserved")
	}
	return nil
}

func zipSignature(root *os.Root, name string, segmented bool) bool {
	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	var head [4]byte
	if _, err := f.ReadAt(head[:], 0); err != nil {
		return false
	}
	if segmented {
		return string(head[:]) == "PK\x07\x08"
	}
	return string(head[:]) == "PK\x03\x04" || string(head[:]) == "PK\x05\x06"
}

// Offsets identify local headers, not payload starts. Bound the minimum
// header/packed extent without inventing encoded-name or extra-field lengths.
// The maintained reader and each member's extraction checks validate the bytes.
func zipMemberBounds(entries []sevenEntry, infos []fs.FileInfo, total int64) bool {
	starts := make([]int64, len(infos))
	for i := 1; i < len(infos); i++ {
		starts[i] = starts[i-1] + infos[i-1].Size()
	}
	for _, e := range entries {
		if e.malformed || !e.sizeKnown || !e.volumeKnown || e.volume >= int64(len(infos)) || !e.offsetKnown || !e.packedKnown || e.packed > total || e.offset > infos[e.volume].Size()-30 {
			return false
		}
		start := starts[e.volume] + e.offset + 30
		if start > total || e.packed > total-start {
			return false
		}
	}
	return true
}
