//go:build !windows

package safeio

import "path/filepath"

func resolvedRootPath(name string) (string, error) { return filepath.EvalSymlinks(name) }
