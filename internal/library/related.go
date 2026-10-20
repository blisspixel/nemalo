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
	Total         int            `json:"total_groups"`
	Offset        int            `json:"offset"`
	Groups        []RelatedGroup `json:"groups"`
	Limitations   []string       `json:"limitations"`
}

// RelatedRequest selects a page of groups, optionally for one holding.
type RelatedRequest struct {
	Directory string
	Holding   string
	Limit     int
	Offset    int
}

// MaxRelatedGroups bounds one page of groups.
const MaxRelatedGroups = 200

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
func Related(ctx context.Context, request RelatedRequest) (RelatedResult, error) {
	directory, holding := request.Directory, request.Holding
	r := RelatedResult{SchemaVersion: 1, Holding: holding, Offset: request.Offset, Groups: []RelatedGroup{}, Limitations: []string{
		"A group means the holdings share one stated identifier, not that they hold the same text, edition, or bytes. Nothing is merged.",
		"Identifiers are copied from EPUB packages and from source metadata at acquisition, and are not confirmed against a registry. Titles, authors, and text are never compared.",
		"Files of one source item can be different editions or formats; versions of one paper are distinct works of record. Tracks of a recording are listed by library recordings, not here.",
		"An ISBN-10 and the ISBN-13 that carries it relate as one edition, under the ISBN-13.",
		"User narration relationships are explicit library-scoped assertions over matching ordered-asset selections. They remain separate from provider narration evidence and establish no rights or scan disposition.",
	}}
	if request.Limit < 1 || request.Limit > MaxRelatedGroups || request.Offset < 0 {
		return r, errors.New("related takes a limit of 1 to 200 groups and an offset of 0 or more")
	}
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
	held := false
	for _, h := range latestList(lines) {
		held = held || h.ID == holding
		if err := ctx.Err(); err != nil {
			return r, err
		}
		for _, id := range h.Identities {
			if rel := relationOf[id.Role]; rel != "" {
				add(key{rel, editionKey(id.ID)}, h, "epub package states "+id.ID, 0)
			}
		}
		for _, s := range h.Sources {
			for _, id := range s.Identities {
				if rel := relationOf[id.Role]; rel != "" {
					add(key{rel, editionKey(id.ID)}, h, s.SourceID+" metadata "+id.Scheme+" states "+id.ID, 0)
				}
			}
			if paper, version, ok := discovery.SplitArxivID(s.SourceID); ok && version > 0 {
				add(key{RelationPaperVersions, "arxiv:" + paper}, h, "acquired as "+s.SourceID, version)
			} else if s.SourceID != "" && s.SourceFile != "" && s.Recording == nil && (discovery.ValidArchiveID(s.SourceID) || discovery.ValidGutenbergID(s.SourceID)) {
				add(key{RelationSourceItemFiles, s.SourceID}, h, "acquired as "+s.SourceID+" file "+s.SourceFile, 0)
			}
			if s.Narration != nil {
				add(key{RelationNarration, s.Narration.ID}, h, "narration "+s.Narration.ID+" from "+s.SourceID, 0)
			}
		}
	}
	if holding != "" && !held {
		return r, errors.New("the library holds no asset " + holding)
	}
	view, err := readNarrationJournal(control.root, state.LibraryID)
	if err != nil {
		return r, err
	}
	if len(view.declarations) > 0 {
		base := RecordingsResult{Library: root, LibraryID: state.LibraryID, Recordings: []RecordingManifest{}, Narrations: []NarrationGroup{}}
		base, err = recordingManifests(ctx, base, lines, "")
		if err != nil {
			return r, err
		}
		if err = decorateUserNarrations(ctx, &base, view); err != nil {
			return r, err
		}
		current := latestHoldings(lines)
		manifests := recordingSelectionIndex(base.Recordings)
		for _, statement := range base.Declarations {
			if !statement.Active || statement.Status != "matched" {
				continue
			}
			for _, selection := range statement.Selections {
				for _, track := range manifests[narrationSelectionKey(selection.Selection)].Tracks {
					add(key{RelationNarration, statement.Declaration.NarrationID}, current[track.AssetID], UserNarrationEvidence+" "+statement.ID, 0)
				}
			}
		}
	}
	all := []RelatedGroup{}
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
		all = append(all, g)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Relation != all[j].Relation {
			return relationRank(all[i].Relation) < relationRank(all[j].Relation)
		}
		return all[i].Key < all[j].Key
	})
	r.Total = len(all)
	if request.Offset < len(all) {
		r.Groups = all[request.Offset:min(len(all), request.Offset+request.Limit)]
	}
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
