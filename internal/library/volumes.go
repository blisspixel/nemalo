package library

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/blisspixel/nemalo/internal/staging"
)

// volumeSet is one numbered family, ordered from its first volume. A reason
// explains why every member must stay instead of being read as a set.
type volumeSet struct {
	volumes  []string
	complete bool
	reason   string
}

// volumeSets groups inventory paths for both ingest and cleanup. Old-style
// first volumes participate only when a continuation is present. Families
// sharing a path are ambiguous; no later group can override that refusal.
func volumeSets(paths []string) []volumeSet {
	type volume struct {
		path   string
		number int
	}
	key := func(p string) string {
		return filepath.Join(filepath.Dir(p), strings.ToLower(filepath.Base(p)))
	}
	sets, firsts := map[string][]volume{}, map[string][]string{}
	for _, p := range paths {
		if set, n, ok := staging.VolumeOf(p); ok {
			sets["numbered:"+key(set)] = append(sets["numbered:"+key(set)], volume{p, n})
		}
		if strings.EqualFold(filepath.Ext(p), ".rar") {
			firsts[key(p)] = append(firsts[key(p)], p)
		}
		if set, n, ok := staging.OldRARVolumeOf(p); ok {
			sets["old:"+key(set)] = append(sets["old:"+key(set)], volume{p, n})
		}
	}
	for name, vs := range sets {
		if set, old := strings.CutPrefix(name, "old:"); old {
			for _, p := range firsts[set] {
				vs = append(vs, volume{p, 1})
			}
			sets[name] = vs
		}
	}
	names := make([]string, 0, len(sets))
	for name := range sets {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []volumeSet
	uses := map[string]int{}
	for _, name := range names {
		vs := sets[name]
		sort.Slice(vs, func(i, j int) bool {
			if vs[i].number != vs[j].number {
				return vs[i].number < vs[j].number
			}
			return vs[i].path < vs[j].path
		})
		if len(vs) == 1 && vs[0].number == 1 && strings.HasSuffix(strings.ToLower(vs[0].path), ".rar") {
			continue
		}
		set := volumeSet{complete: len(vs) >= 2}
		for i, v := range vs {
			set.complete = set.complete && v.number == i+1
			set.volumes = append(set.volumes, v.path)
			uses[v.path]++
			if i > 0 && vs[i-1].number == v.number {
				set.reason = "a volume of a set with ambiguous names or duplicate ordinals; preserved"
			}
		}
		if len(vs) > 1000 {
			set.complete = false
			set.reason = "a volume of a set exceeding the 1,000-volume limit; preserved"
		}
		if !set.complete && set.reason == "" {
			set.reason = "a volume of a set with a missing volume; preserved"
		}
		out = append(out, set)
	}
	for i := range out {
		for _, p := range out[i].volumes {
			if uses[p] > 1 {
				out[i].complete = false
				out[i].reason = "a volume shared by overlapping numbering families; preserved"
			}
		}
	}
	return out
}
