package staging

import (
	"context"
	"fmt"
	"io"
)

// Version selection uses only the checked private snapshot's signature and
// explicit caller capabilities. Neither version grants the other's evidence.
type rarSetBinding struct {
	allow4, allow5 bool
	old            bool
	version        int
	four           rar4SetBinding
	five           rar5SetBinding
	entries        []sevenEntry
}

func (p *rarSetBinding) coverageKind(kind string) error {
	if kind == "Rar" && p.allow4 {
		p.version = 4
		return nil
	}
	if kind == "Rar5" && p.allow5 {
		p.version = 5
		return nil
	}
	if p.allow5 {
		return fmt.Errorf("%w: RAR4 set coverage is unavailable in this operation", ErrRAR5Coverage)
	}
	return fmt.Errorf("%w: RAR5 set coverage is unavailable in this operation", ErrRAR4Coverage)
}

func (p *rarSetBinding) accountVolume(ctx context.Context, kind string, source io.ReaderAt, size int64, entries []sevenEntry, index, count, members int) error {
	if kind == "Rar" {
		v, err := rar4VolumeAccounted(ctx, source, size, p.old)
		if err != nil {
			return err
		}
		if !rar4VolumeTableMatches(ctx, v, entries) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%w: isolated table disagrees with physical volume", ErrRAR4Coverage)
		}
		return p.four.add(ctx, v, index, count, members)
	}
	v, err := RAR5VolumeAccounted(ctx, source, size)
	if err != nil {
		return err
	}
	if !rar5VolumeTableMatches(ctx, v, entries) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: isolated table disagrees with physical volume", ErrRAR5Coverage)
	}
	return p.five.add(ctx, v, index, count, members)
}

func (p *rarSetBinding) matchesAssembled(ctx context.Context, entries []sevenEntry) bool {
	if p.version == 4 {
		return p.four.matchesAssembled(ctx, entries)
	}
	return p.version == 5 && p.five.matchesAssembled(ctx, entries)
}
