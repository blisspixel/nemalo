package release

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Dependency struct {
	Path, Version, Directory string
}

// Notices retains upstream license/notice text for the binary's actual modules.
// Dependency directories come from the pinned Go module graph, not user content.
func Notices(goVersion string, goLicense []byte, dependencies []Dependency) ([]byte, error) {
	if goVersion == "" || len(goLicense) == 0 {
		return nil, errors.New("missing Go standard library license")
	}
	deps := append([]Dependency(nil), dependencies...)
	sort.Slice(deps, func(i, j int) bool { return deps[i].Path < deps[j].Path })
	var out bytes.Buffer
	fmt.Fprintf(&out, "Third-party notices\n\nGo standard library %s\n\n%s\n", goVersion, goLicense)
	for i, dep := range deps {
		if dep.Path == "" || dep.Version == "" || dep.Directory == "" || (i > 0 && deps[i-1].Path == dep.Path) {
			return nil, errors.New("invalid or duplicate release dependency")
		}
		entries, err := os.ReadDir(dep.Directory)
		if err != nil {
			return nil, err
		}
		foundLicense := false
		fmt.Fprintf(&out, "\n%s %s\n", dep.Path, dep.Version)
		for _, entry := range entries {
			name := strings.ToUpper(entry.Name())
			license := name == "LICENSE" || strings.HasPrefix(name, "LICENSE.") || name == "COPYING" || strings.HasPrefix(name, "COPYING.")
			if !license && name != "NOTICE" && !strings.HasPrefix(name, "NOTICE.") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 {
				return nil, fmt.Errorf("invalid license/notice file for %s", dep.Path)
			}
			data, err := os.ReadFile(filepath.Join(dep.Directory, entry.Name()))
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&out, "\n%s\n\n%s\n", entry.Name(), data)
			foundLicense = foundLicense || license
		}
		if !foundLicense {
			return nil, fmt.Errorf("missing upstream license for %s", dep.Path)
		}
	}
	return out.Bytes(), nil
}
