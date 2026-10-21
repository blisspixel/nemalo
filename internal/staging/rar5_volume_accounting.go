package staging

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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
	rarSetFiles
}

// add closes only the continuation at an adjacent volume boundary. It never
// joins payloads or equates packed-part CRCs with the final whole-file CRC.
func (s *rar5SetBinding) add(ctx context.Context, v RAR5Volume, index, count, members int) error {
	parts, err := rar5SetParts(ctx, v)
	if err != nil {
		return err
	}
	return s.rarSetFiles.add(ctx, v.Ordinal, v.More, v.Solid, parts, index, count, members, ErrRAR5Coverage)
}

func rar5VolumeTableMatches(ctx context.Context, volume RAR5Volume, entries []sevenEntry) bool {
	parts, err := rar5SetParts(ctx, volume)
	return err == nil && rarVolumeTableMatches(ctx, volume.Ordinal, parts, entries)
}

func rar5SetParts(ctx context.Context, volume RAR5Volume) ([]rarSetPart, error) {
	parts := make([]rarSetPart, len(volume.Files))
	for i, f := range volume.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// The format tag prevents equal metadata bytes crossing versions.
		metadata := []byte{5}
		for _, n := range []uint64{f.Compression, f.HostOS, f.Attributes, f.metadata.flags, uint64(f.metadata.modified)} {
			metadata = binary.LittleEndian.AppendUint64(metadata, n)
		}
		metadata = append(metadata, f.metadata.extra[:]...)
		parts[i] = rarSetPart{
			file:   rarBindingFile{Name: f.Name, Bytes: f.Bytes, Packed: f.Packed, CRC32: f.CRC32, HasCRC: f.HasCRC, Directory: f.Directory},
			before: f.SplitBefore, after: f.SplitAfter, stored: f.Compression>>7&7 == 0,
			metadata: sha256.Sum256(metadata),
		}
	}
	return parts, ctx.Err()
}
