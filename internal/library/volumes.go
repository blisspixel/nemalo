package library

import (
	"sort"
	"strings"

	"github.com/blisspixel/nemalo/internal/staging"
)

// volumeSet is one set of archive volumes among a folder's files, in volume
// order. It is complete when numbered from 1 without a gap and holds at
// least two volumes.
type volumeSet struct {
	volumes  []string
	complete bool
}

// volumeSets groups slash paths that name archive volumes, in set order. A
// lone .part1.rar may be a whole archive, so it is not a set and is read as
// one archive.
func volumeSets(paths []string) []volumeSet {
	type volume struct {
		path   string
		number int
	}
	sets := map[string][]volume{}
	for _, p := range paths {
		if set, n, ok := staging.VolumeOf(p); ok {
			sets[set] = append(sets[set], volume{p, n})
		}
	}
	names := make([]string, 0, len(sets))
	for name := range sets {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []volumeSet
	for _, name := range names {
		vs := sets[name]
		sort.Slice(vs, func(i, j int) bool { return vs[i].number < vs[j].number })
		if len(vs) == 1 && vs[0].number == 1 && strings.HasSuffix(strings.ToLower(vs[0].path), ".rar") {
			continue
		}
		set := volumeSet{complete: len(vs) >= 2}
		for i, v := range vs {
			set.complete = set.complete && v.number == i+1
			set.volumes = append(set.volumes, v.path)
		}
		out = append(out, set)
	}
	return out
}
