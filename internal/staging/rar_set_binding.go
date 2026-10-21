package staging

import (
	"context"
	"fmt"
)

// Each format supplies its own declaration fingerprint. These common facts
// bind split records; they are neither decoded integrity nor write authority.
type rarSetPart struct {
	file          rarBindingFile
	before, after bool
	stored        bool
	metadata      [32]byte
}

type rarSetFiles struct {
	files  []rarBindingFile
	starts []int
	last   *rarSetPart
	solid  bool
}

func (s *rarSetFiles) add(ctx context.Context, ordinal int, more, solid bool, parts []rarSetPart, index, count, members int, coverageErr error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bad := func(detail string) error { return fmt.Errorf("%w: %s", coverageErr, detail) }
	if ordinal != index || more != (index+1 < count) || len(parts) == 0 || (index > 0 && solid != s.solid) {
		return bad("incompatible physical volume role, ordinal or solid declaration")
	}
	if index == 0 {
		s.solid = solid
	}
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if part.before != (i == 0 && index > 0) || part.after != (i+1 == len(parts) && index+1 < count) || (part.file.Directory && (part.before || part.after)) {
			return bad("unlinked physical split-member boundary")
		}
		if part.before {
			previous := s.last
			if previous == nil || !previous.after || previous.file.Name != part.file.Name || previous.file.Bytes <= 0 || previous.file.Bytes != part.file.Bytes || previous.metadata != part.metadata || previous.file.Directory != part.file.Directory || previous.file.HasCRC != part.file.HasCRC {
				return bad("continuation metadata does not agree")
			}
			file := &s.files[len(s.files)-1]
			if part.file.Packed > maxArchiveSnapshot-file.Packed {
				return bad("combined packed size exceeds bounds")
			}
			file.Packed += part.file.Packed
			file.CRC32 = part.file.CRC32
		} else {
			if len(s.files) >= members {
				return bad("complete member limit")
			}
			s.files = append(s.files, part.file)
			s.starts = append(s.starts, index)
		}
		file := s.files[len(s.files)-1]
		if !part.after && part.stored && file.Packed != file.Bytes {
			return bad("complete stored member size does not agree")
		}
		copy := part
		s.last = &copy
	}
	if index+1 == count {
		names := map[string]bool{}
		for _, file := range s.files {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, exists := names[file.Name]; exists {
				return errRARNameConflict
			}
			names[file.Name] = file.Directory
		}
		return unambiguousRARParents(ctx, names)
	}
	return ctx.Err()
}

func rarVolumeTableMatches(ctx context.Context, ordinal int, parts []rarSetPart, entries []sevenEntry) bool {
	if ctx.Err() != nil || len(parts) != len(entries) {
		return false
	}
	files := make([]rarBindingFile, len(parts))
	byName := make(map[string]rarSetPart, len(parts))
	for i, part := range parts {
		if ctx.Err() != nil {
			return false
		}
		files[i] = part.file
		byName[part.file.Name] = part
	}
	normalized := make([]sevenEntry, len(entries))
	for i, entry := range entries {
		if ctx.Err() != nil {
			return false
		}
		part, ok := byName[entry.raw]
		if !ok || entry.splitFields != 3 || entry.splitBefore != part.before || entry.splitAfter != part.after || !entry.volumeKnown || entry.volume != int64(ordinal) {
			return false
		}
		entry.split, entry.splitBefore, entry.splitAfter = false, false, false
		normalized[i] = entry
	}
	return rarTableMatches(ctx, files, normalized)
}

func (s *rarSetFiles) matchesAssembled(ctx context.Context, entries []sevenEntry) bool {
	if !rarTableMatches(ctx, s.files, entries) {
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
