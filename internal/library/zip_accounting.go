package library

import (
	"context"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/staging"
	"os"
	"path/filepath"
)

// zipAccounted checks byte coverage separately from decoding and member checks.
func zipAccounted(ctx context.Context, src *os.Root, rel string) (bool, string) {
	name := filepath.FromSlash(rel)
	info, err := src.Lstat(name)
	if err != nil {
		return false, "the archive could not be read"
	}
	f, err := safeio.OpenRegular(src, name, info, os.O_RDONLY)
	if err != nil {
		return false, "the archive could not be read"
	}
	defer f.Close()
	return staging.ZIPAccounted(ctx, f, info.Size())
}
