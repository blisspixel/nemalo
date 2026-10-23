package library

import "github.com/blisspixel/nemalo/internal/staging"

type volumeSet struct {
	volumes  []string
	complete bool
	reason   string
}

// Project the shared grouping without duplicating its family policy.
func volumeSets(paths []string) []volumeSet {
	var sets []volumeSet
	for _, group := range staging.VolumeSets(paths) {
		sets = append(sets, volumeSet{volumes: group.Volumes, complete: group.Complete, reason: group.Reason})
	}
	return sets
}
