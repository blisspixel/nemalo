package staging

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Cancellation arrives after the final read's own context check, at the next
// check in name validation. This avoids a timing-dependent large-input test.
type cancelAtRARNames struct {
	context.Context
	cancel         context.CancelFunc
	armed          bool
	checksAfterEnd int
}

func (c *cancelAtRARNames) Err() error {
	if c.armed {
		c.checksAfterEnd++
		if c.checksAfterEnd == 2 {
			c.cancel()
		}
	}
	return c.Context.Err()
}

type armRARNames struct {
	source *bytes.Reader
	ctx    *cancelAtRARNames
	size   int64
}

func (r armRARNames) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.source.ReadAt(p, off)
	if err == nil && off+int64(len(p)) == r.size {
		r.ctx.armed = true
	}
	return n, err
}
func TestRARCancellationDuringFinalNameValidationReturnsNoEvidence(t *testing.T) {
	name := strings.Repeat("folder/", 100) + "book.epub"
	for _, version := range []int{4, 5} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			data := rar4Archive(rar4Book(name))
			if version == 5 {
				data = rar5Archive(rar5Book(name))
			}
			inner, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &cancelAtRARNames{Context: inner, cancel: cancel}
			reader := armRARNames{bytes.NewReader(data), ctx, int64(len(data))}
			if version == 4 {
				files, err := RAR4Accounted(ctx, reader, int64(len(data)))
				if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrRAR4Coverage) || files != nil {
					t.Fatal("late cancellation granted RAR4 evidence", len(files), err, ctx.checksAfterEnd)
				}
			} else {
				files, err := RAR5Accounted(ctx, reader, int64(len(data)))
				if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrRAR5Coverage) || files != nil {
					t.Fatal("late cancellation granted RAR5 evidence", len(files), err, ctx.checksAfterEnd)
				}
			}
		})
	}
}
