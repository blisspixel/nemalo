package staging

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

// RAR4Volume describes physical framing, not a complete set or payload integrity.
type RAR4Volume struct {
	Ordinal            int
	First, More, Solid bool
	Files              []RAR4VolumeFile
}

type RAR4VolumeFile struct {
	RAR4File
	SplitBefore, SplitAfter bool
	metadata                rar4VolumeMetadata
}

type rar4VolumeMetadata struct {
	flags uint16
	extra [32]byte
}

// RAR4VolumeAccounted measures exact physical spans without decoding or reading
// payloads. Set callers must additionally share Limits.proofReader across parts.
// RAR4Accounted retains its refusal of every volume and split declaration.
func RAR4VolumeAccounted(ctx context.Context, source io.ReaderAt, size int64) (RAR4Volume, error) {
	volume := RAR4Volume{}
	if _, err := rar4Accounted(ctx, source, size, &volume); err != nil {
		return RAR4Volume{}, err
	}
	return volume, nil
}

type rar4SetBinding struct{ rarSetFiles }

func (s *rar4SetBinding) add(ctx context.Context, v RAR4Volume, index, count, members int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.First != (index == 0) {
		return fmt.Errorf("%w: first volume role disagrees with set position", ErrRAR4Coverage)
	}
	parts, err := rar4SetParts(ctx, v)
	if err != nil {
		return err
	}
	return s.rarSetFiles.add(ctx, v.Ordinal, v.More, v.Solid, parts, index, count, members, ErrRAR4Coverage)
}

func rar4VolumeTableMatches(ctx context.Context, volume RAR4Volume, entries []sevenEntry) bool {
	parts, err := rar4SetParts(ctx, volume)
	return err == nil && rarVolumeTableMatches(ctx, volume.Ordinal, parts, entries)
}

func rar4SetParts(ctx context.Context, volume RAR4Volume) ([]rarSetPart, error) {
	parts := make([]rarSetPart, len(volume.Files))
	for i, f := range volume.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		metadata := []byte{4, f.HostOS, f.UnpackVersion, f.CompressionMethod}
		metadata = binary.LittleEndian.AppendUint16(metadata, f.metadata.flags)
		metadata = binary.LittleEndian.AppendUint32(metadata, f.Attributes)
		metadata = binary.LittleEndian.AppendUint32(metadata, f.ModifiedDOS)
		metadata = append(metadata, f.metadata.extra[:]...)
		parts[i] = rarSetPart{
			file:   rarBindingFile{Name: f.Name, Bytes: f.Bytes, Packed: f.Packed, CRC32: f.CRC32, HasCRC: true, Directory: f.Directory},
			before: f.SplitBefore, after: f.SplitAfter, stored: f.CompressionMethod == 0x30,
			metadata: sha256.Sum256(metadata),
		}
	}
	return parts, ctx.Err()
}
