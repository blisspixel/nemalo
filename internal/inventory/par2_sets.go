package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
)

type par2Group struct {
	index                      int
	mains, descriptions, maker map[string]int
	fileIDs, names             map[string]string
}

func (r *par2Run) add(file string, metadata *PAR2File, observations []par2Observation) {
	for _, o := range observations {
		g := r.groups[o.set]
		if g == nil {
			g = &par2Group{index: len(r.report.Sets), mains: map[string]int{}, descriptions: map[string]int{}, maker: map[string]int{}, fileIDs: map[string]string{}, names: map[string]string{}}
			r.groups[o.set] = g
			r.report.Sets = append(r.report.Sets, PAR2Set{ID: o.set, Files: []string{}, Mains: []PAR2Main{}, Descriptions: []PAR2Description{}, Creators: []PAR2Creator{}, Conflicts: []string{}})
		}
		set := &r.report.Sets[g.index]
		if !containsString(set.Files, file) {
			set.Files = append(set.Files, file)
		}
		set.Packets++
		switch o.kind {
		case par2RecoveryType:
			set.ParityHeaders++
		case par2InputType:
			set.InputHeaders++
		case par2MainType, par2DescType, par2CreatorType:
		default:
			set.UnknownHeaders++
		}
		switch o.checksum {
		case "mismatched":
			set.Mismatched++
		case "unchecked":
			set.Unchecked++
		case "matching":
			set.Matching++
			location := PAR2Location{file, o.offset}
			// This key compares decoded declaration variants, not protected
			// files or the format's weak checksum as an identity.
			sum := sha256.Sum256(o.body)
			key := hex.EncodeToString(sum[:])
			switch o.kind {
			case par2MainType:
				if index, seen := g.mains[key]; seen {
					set.Mains[index].Locations = append(set.Mains[index].Locations, location)
					continue
				}
				m, err := mainDeclaration(o.body)
				if err != nil {
					par2Problem(metadata, o.offset, err)
					continue
				}
				g.mains[key] = len(set.Mains)
				m.Locations = []PAR2Location{location}
				set.Mains = append(set.Mains, m)
				if len(set.Mains) > 1 {
					addConflict(set, "conflicting_main_packets")
				}
			case par2DescType:
				d, err := fileDeclaration(o.body)
				if err != nil {
					par2Problem(metadata, o.offset, err)
					continue
				}
				if index, seen := g.descriptions[key]; seen {
					set.Descriptions[index].Locations = append(set.Descriptions[index].Locations, location)
					continue
				}
				if previous, seen := g.fileIDs[d.FileID]; seen && previous != key {
					addConflict(set, "conflicting_file_descriptions")
				}
				name := "utf8:" + d.Name
				if d.NameHex != "" {
					name = "bytes:" + d.NameHex
				}
				if id, seen := g.names[name]; seen && id != d.FileID {
					addConflict(set, "filename_declared_for_multiple_file_ids")
				}
				g.fileIDs[d.FileID], g.names[name] = key, d.FileID
				g.descriptions[key] = len(set.Descriptions)
				d.Locations = []PAR2Location{location}
				set.Descriptions = append(set.Descriptions, d)
			case par2CreatorType:
				if index, seen := g.maker[key]; seen {
					set.Creators[index].Locations = append(set.Creators[index].Locations, location)
					continue
				}
				g.maker[key] = len(set.Creators)
				creator := creatorDeclaration(o.body)
				creator.Locations = []PAR2Location{location}
				set.Creators = append(set.Creators, creator)
			}
		}
	}
}

func addConflict(set *PAR2Set, detail string) {
	if !containsString(set.Conflicts, detail) {
		set.Conflicts = append(set.Conflicts, detail)
	}
}

func (r *par2Run) finish() {
	slices.SortFunc(r.report.Sets, func(a, b PAR2Set) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
}
