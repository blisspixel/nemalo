package staging

import (
	"context"
	"fmt"
	"io"
)

// RAR5Volume is framing evidence for one physical volume, not a complete set,
// checked payload or write permission. More is the declared end-volume role.
type RAR5Volume struct {
	Ordinal     int
	More, Solid bool
	Files       []RAR5VolumeFile
}

// RAR5VolumeFile keeps split roles separate from the standalone file facts.
// A SplitAfter CRC describes this part's packed bytes; otherwise it describes
// the whole unpacked member. Neither declaration verifies those bytes.
type RAR5VolumeFile struct {
	RAR5File
	SplitBefore, SplitAfter bool
	metadata                rar5VolumeMetadata
}

type rar5VolumeMetadata struct {
	flags    uint64
	modified uint32
	extra    [32]byte
}

// RAR5VolumeAccounted measures known framing through exact EOF, without reading
// payloads or decoding. Set callers must share Limits.proofReader across parts.
// The standalone RAR5Accounted entry point still refuses every volume/split flag.
func RAR5VolumeAccounted(ctx context.Context, source io.ReaderAt, size int64) (RAR5Volume, error) {
	volume := RAR5Volume{}
	if _, err := rar5Accounted(ctx, source, size, &volume); err != nil {
		return RAR5Volume{}, err
	}
	return volume, nil
}

type rar5SetBinding struct {
	files   []RAR5File
	starts  []int
	last    *RAR5VolumeFile
	solid   bool
	entries []sevenEntry
}

// add closes only the continuation at an adjacent volume boundary. It never
// joins payloads or equates packed-part CRCs with the final whole-file CRC.
func (s *rar5SetBinding) add(ctx context.Context, v RAR5Volume, index, count, members int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bad := func(detail string) error { return fmt.Errorf("%w: %s", ErrRAR5Coverage, detail) }
	if v.Ordinal != index || v.More != (index+1 < count) || len(v.Files) == 0 || (index > 0 && v.Solid != s.solid) {
		return bad("incompatible physical volume role, ordinal or solid declaration")
	}
	if index == 0 {
		s.solid = v.Solid
	}
	for i, part := range v.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if part.SplitBefore != (i == 0 && index > 0) || part.SplitAfter != (i+1 == len(v.Files) && index+1 < count) || (part.Directory && (part.SplitBefore || part.SplitAfter)) {
			return bad("unlinked physical split-member boundary")
		}
		if part.SplitBefore {
			previous := s.last
			if previous == nil || !previous.SplitAfter || previous.Name != part.Name || previous.Bytes <= 0 || previous.Bytes != part.Bytes || previous.Compression != part.Compression || previous.HostOS != part.HostOS || previous.Attributes != part.Attributes || previous.metadata != part.metadata || previous.Directory != part.Directory || previous.HasCRC != part.HasCRC {
				return bad("continuation metadata does not agree")
			}
			file := &s.files[len(s.files)-1]
			if part.Packed > maxArchiveSnapshot-file.Packed {
				return bad("combined packed size exceeds bounds")
			}
			file.Packed += part.Packed
			file.CRC32 = part.CRC32
		} else {
			if len(s.files) >= members {
				return bad("complete member limit")
			}
			s.files = append(s.files, part.RAR5File)
			s.starts = append(s.starts, index)
		}
		file := s.files[len(s.files)-1]
		if !part.SplitAfter && file.Compression>>7&7 == 0 && file.Packed != file.Bytes {
			return bad("complete stored member size does not agree")
		}
		copy := part
		s.last = &copy
	}
	if index+1 == count {
		return unambiguousRAR5Names(ctx, s.files)
	}
	return ctx.Err()
}

func rar5VolumeTableMatches(ctx context.Context, volume RAR5Volume, entries []sevenEntry) bool {
	if len(volume.Files) != len(entries) {
		return false
	}
	files := make([]RAR5File, len(volume.Files))
	byName := make(map[string]RAR5VolumeFile, len(files))
	for i, part := range volume.Files {
		if ctx.Err() != nil {
			return false
		}
		files[i] = part.RAR5File
		byName[part.Name] = part
	}
	normalized := make([]sevenEntry, len(entries))
	for i, entry := range entries {
		if ctx.Err() != nil {
			return false
		}
		part, ok := byName[entry.raw]
		if !ok || entry.splitFields != 3 || entry.splitBefore != part.SplitBefore || entry.splitAfter != part.SplitAfter || !entry.volumeKnown || entry.volume != int64(volume.Ordinal) {
			return false
		}
		entry.split, entry.splitBefore, entry.splitAfter = false, false, false
		normalized[i] = entry
	}
	return rarTableMatches(ctx, rar5BindingFiles(files), normalized)
}

func (s *rar5SetBinding) matchesAssembled(ctx context.Context, entries []sevenEntry) bool {
	if !rarTableMatches(ctx, rar5BindingFiles(s.files), entries) {
		return false
	}
	starts := make(map[string]int, len(s.files))
	for i, file := range s.files {
		if ctx.Err() != nil {
			return false
		}
		starts[file.Name] = s.starts[i]
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return false
		}
		if entry.splitFields != 3 || !entry.volumeKnown || entry.volume != int64(starts[entry.raw]) {
			return false
		}
	}
	return true
}
