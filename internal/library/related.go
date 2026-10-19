package library

import (
	"context"
	"errors"
	"path/filepath"
	"sort"

	"github.com/blisspixel/nemalo/internal/discovery"
)

// Relations between holdings. Each names the evidence that relates them;
// none says two holdings are the same bytes, edition, or text.
const (
	RelationSameWork        = "same_work"
	RelationSameEdition     = "same_edition"
	RelationSameVersion     = "same_publication_version"
	RelationPaperVersions   = "versions_of_one_paper"
	RelationSourceItemFiles = "files_of_one_source_item"
	RelationNarration       = "one_narration"
)

// RelatedResult groups holdings that share one explicit identifier. A
// holding can appear in several groups; groups are never merged.
type RelatedResult struct {
	SchemaVersion int            `json:"schema_version"`
	Library       string         `json:"library"`
	LibraryID     string         `json:"library_id"`
	Holding       string         `json:"holding,omitempty"`
	Groups        []RelatedGroup `json:"groups"`
	Limitations   []string       `json:"limitations"`
}

// RelatedGroup is every holding that shares Key, with how each states it.
type RelatedGroup struct {
	Key      string          `json:"key"`
	Relation string          `json:"relation"`
	Members  []RelatedMember `json:"members"`
}

// RelatedMember is one holding of a group and the statements that place it.
type RelatedMember struct {
	Holding  string   `json:"holding"`
	Format   string   `json:"format"`
	Title    string   `json:"title,omitempty"`
	Version  int      `json:"version,omitempty"`
	Evidence []string `json:"evidence"`
}

// Related reads the journal and groups holdings that share an identifier
// stated by an EPUB package or a source's metadata, an arXiv paper, a source
// item, or a narration. Titles, names, and text are never compared. With
// holding, only that holding's groups are returned.
func Related(ctx context.Context, directory, holding string) (RelatedResult, error) {
	r := RelatedResult{SchemaVersion: 1, Holding: holding, Groups: []RelatedGroup{}, Limitations: []string{
		"A group means the holdings share one stated identifier, not that they hold the same text, edition, or bytes. Nothing is merged.",
		"Identifiers are copied from EPUB packages and from source metadata at acquisition, and are not confirmed against a registry. Titles, authors, and text are never compared.",
		"Files of one source item can be different editions or formats; versions of one paper are distinct works of record.",
	}}
	if holding != "" && !validAssetID(holding) {
		return r, errors.New("holding must be an asset ID, sha256: followed by 64 lowercase hex digits")
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return r, err
	}
	r.Library = root
	state, err := LibraryStatus(ctx, root)
	if err != nil {
		return r, err
	}
	if state.Status != "initialized" {
		return r, errors.New("related holdings require an initialized library; run library init on this explicit directory")
	}
	r.LibraryID = state.LibraryID
	control, _, err := openControl(root, false)
	if err != nil {
		return r, err
	}
	defer control.close()
	_, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return r, err
	}
	type key struct{ relation, id string }
	groups := map[key]map[string]*RelatedMember{}
	add := func(k key, h Holding, evidence string, version int) {
		members := groups[k]
		if members == nil {
			members = map[string]*RelatedMember{}
			groups[k] = members
		}
		m := members[h.ID]
		if m == nil {
			m = &RelatedMember{Holding: h.ID, Format: h.Format, Evidence: []string{}}
			if len(h.Titles) > 0 {
				m.Title = h.Titles[0]
			}
			members[h.ID] = m
		}
		if version > m.Version {
			m.Version = version
		}
		for _, e := range m.Evidence {
			if e == evidence {
				return
			}
		}
		m.Evidence = append(m.Evidence, evidence)
	}
	relationOf := map[string]string{"work": RelationSameWork, "edition": RelationSameEdition, "publication_version": RelationSameVersion}
	for _, h := range latestList(lines) {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		for _, id := range h.Identities {
			if rel := relationOf[id.Role]; rel != "" {
				add(key{rel, id.ID}, h, "epub package states "+id.ID, 0)
			}
		}
		for _, s := range h.Sources {
			for _, id := range s.Identities {
				if rel := relationOf[id.Role]; rel != "" {
					add(key{rel, id.ID}, h, s.SourceID+" metadata "+id.Scheme+" states "+id.ID, 0)
				}
			}
			if paper, version, ok := discovery.SplitArxivID(s.SourceID); ok && version > 0 {
				add(key{RelationPaperVersions, "arxiv:" + paper}, h, "acquired as "+s.SourceID, version)
			} else if s.SourceID != "" && s.SourceFile != "" && (discovery.ValidArchiveID(s.SourceID) || discovery.ValidGutenbergID(s.SourceID)) {
				add(key{RelationSourceItemFiles, s.SourceID}, h, "acquired as "+s.SourceID+" file "+s.SourceFile, 0)
			}
			if s.Narration != nil {
				add(key{RelationNarration, s.Narration.ID}, h, "narration "+s.Narration.ID+" from "+s.SourceID, 0)
			}
		}
	}
	for k, members := range groups {
		if len(members) < 2 || (holding != "" && members[holding] == nil) {
			continue
		}
		g := RelatedGroup{Key: k.id, Relation: k.relation, Members: make([]RelatedMember, 0, len(members))}
		for _, m := range members {
			sort.Strings(m.Evidence)
			g.Members = append(g.Members, *m)
		}
		sort.Slice(g.Members, func(i, j int) bool {
			if g.Members[i].Version != g.Members[j].Version {
				return g.Members[i].Version < g.Members[j].Version
			}
			return g.Members[i].Holding < g.Members[j].Holding
		})
		r.Groups = append(r.Groups, g)
	}
	sort.Slice(r.Groups, func(i, j int) bool {
		if r.Groups[i].Relation != r.Groups[j].Relation {
			return relationRank(r.Groups[i].Relation) < relationRank(r.Groups[j].Relation)
		}
		return r.Groups[i].Key < r.Groups[j].Key
	})
	return r, nil
}

var relationOrder = []string{RelationSameWork, RelationSameEdition, RelationSameVersion, RelationPaperVersions, RelationNarration, RelationSourceItemFiles}

func relationRank(relation string) int {
	for i, o := range relationOrder {
		if o == relation {
			return i
		}
	}
	return len(relationOrder)
}
