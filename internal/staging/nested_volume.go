package staging

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Targets only relax the nested-volume classification. Every ordinary member
// safety, integrity and work check still applies before a part can be staged.
func nestedVolumeTargets(names []string, limits Limits) map[string]bool {
	if !limits.NestedVolumeSets || !limits.nestedTraversal() {
		return nil
	}
	allowed := map[string]bool{}
	for _, group := range VolumeSets(names) {
		if !group.Complete {
			continue
		}
		valid := true
		for _, name := range group.Volumes {
			valid = valid && len(name) <= 1024 && utf8.ValidString(name) && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:\x00")
		}
		if valid {
			for _, name := range group.Volumes {
				allowed[name] = true
			}
		}
	}
	return allowed
}

type nestedVolumeGroup struct {
	// Ordered is volume order; report is original table order. They contain
	// indices only, never paths to open from archive metadata.
	ordered, report []int
	first           string
	reason          string
	private         []string
}

func nestedMemberGroups(members []Member) map[int]*nestedVolumeGroup {
	names := make([]string, len(members))
	positions := map[string][]int{}
	for i, m := range members {
		names[i] = m.Name
		positions[m.Name] = append(positions[m.Name], i)
	}
	byMember := map[int]*nestedVolumeGroup{}
	for _, set := range VolumeSets(names) {
		g := &nestedVolumeGroup{first: set.Volumes[0], reason: set.Reason}
		seen := map[int]bool{}
		for _, name := range set.Volumes {
			at := positions[name]
			if len(at) != 1 {
				g.reason = "the nested set has ambiguous member names; its contents remain preserved"
			}
			for _, i := range at {
				if seen[i] {
					continue
				}
				seen[i] = true
				g.ordered = append(g.ordered, i)
				if members[i].Disposition != "extracted" || members[i].Staged == "" {
					g.reason = "a nested volume was not uniquely extracted with verified bytes; the whole set remains preserved"
				}
			}
		}
		if !set.Complete && g.reason == "" {
			g.reason = "the nested set is incomplete; its contents remain preserved"
		}
		g.private = nestedPrivateNames(set.Volumes)
		g.report = append([]int(nil), g.ordered...)
		sort.Ints(g.report)
		for _, i := range g.report {
			byMember[i] = g
		}
	}
	return byMember
}

func nestedPrivateNames(names []string) []string {
	if len(names) < 2 {
		return nil
	}
	_, _, old := OldRARVolumeOf(names[1])
	_, _, segmented := ZIPSegmentOf(names[0])
	set, _, _ := VolumeOf(names[0])
	private := make([]string, len(names))
	for i := range names {
		switch {
		case segmented:
			private[i] = fmt.Sprintf("archive.z%02d", i+1)
			if i+1 == len(names) {
				private[i] = "archive.zip"
			}
		case old:
			private[i] = rarSnapshotName(true, i)
		case strings.EqualFold(filepath.Ext(set), ".rar"):
			private[i] = rarSnapshotName(false, i)
		case strings.EqualFold(filepath.Ext(set), ".zip"):
			private[i] = fmt.Sprintf("archive.zip.%04d", i+1)
		default:
			private[i] = fmt.Sprintf("archive.7z.%03d", i+1)
		}
	}
	return private
}

// Only flat names already produced by checked extraction are moved. The set
// reader sees a fresh root with fixed names, never original member paths.
func visitNestedVolumeGroup(ctx context.Context, stage *os.Root, members []Member, group *nestedVolumeGroup, visit func(*os.Root, []string) error) (err error) {
	dir := fmt.Sprintf("set-%05d", group.report[0])
	if err := stage.Mkdir(dir, 0700); err != nil {
		return err
	}
	defer func() {
		for i, at := range group.ordered {
			if name := members[at].Staged; name != "" {
				if removeErr := stage.Remove(name); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					err = errors.Join(err, removeErr)
				}
			}
			if removeErr := stage.Remove(filepath.Join(dir, group.private[i])); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
		}
		err = errors.Join(err, stage.Remove(dir))
	}()
	for i, at := range group.ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := stage.Rename(members[at].Staged, filepath.Join(dir, group.private[i])); err != nil {
			return err
		}
	}
	root, err := stage.OpenRoot(dir)
	if err != nil {
		return err
	}
	err = visit(root, group.private)
	return errors.Join(err, root.Close())
}
