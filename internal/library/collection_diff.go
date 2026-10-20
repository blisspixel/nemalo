package library

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
)

// CollectionRevision identifies the exact bytes read for one declaration.
type CollectionRevision struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Name   string `json:"name"`
	Items  int    `json:"items"`
}

// CollectionPosition locates one selection in its manifest, one-based.
type CollectionPosition struct {
	Position int            `json:"position"`
	Item     CollectionItem `json:"item"`
}

// CollectionNoteChange retains the old note and the after item, including its
// new note. The selection is unchanged; it does not become an acquisition.
type CollectionNoteChange struct {
	Item           CollectionItem `json:"item"`
	BeforeNote     string         `json:"before_note"`
	BeforePosition int            `json:"before_position"`
	AfterPosition  int            `json:"after_position"`
}

// CollectionOrderEntry locates one retained selection in both manifests. Item
// is the after declaration, including its note.
type CollectionOrderEntry struct {
	Item           CollectionItem `json:"item"`
	BeforePosition int            `json:"before_position"`
	AfterPosition  int            `json:"after_position"`
}

// CollectionDiffResult compares declarations, not holdings or upstream content.
// RetainedOrder is empty unless retained selections changed relative order;
// insertions and removals shifting absolute positions alone do not populate it.
type CollectionDiffResult struct {
	SchemaVersion int                    `json:"schema_version"`
	Complete      bool                   `json:"complete"`
	Before        CollectionRevision     `json:"before"`
	After         CollectionRevision     `json:"after"`
	Changed       bool                   `json:"changed"`
	NameChanged   bool                   `json:"name_changed"`
	Added         []CollectionPosition   `json:"added"`
	Removed       []CollectionPosition   `json:"removed"`
	NotesChanged  []CollectionNoteChange `json:"notes_changed"`
	RetainedOrder []CollectionOrderEntry `json:"retained_order"`
	Limitations   []string               `json:"limitations"`
}

// CollectionDiff compares two explicit manifests offline and without writes.
// It returns no partial comparison if either input fails or the call is cancelled.
func CollectionDiff(ctx context.Context, before, after string) (CollectionDiffResult, error) {
	empty := CollectionDiffResult{SchemaVersion: 1, Added: []CollectionPosition{}, Removed: []CollectionPosition{}, NotesChanged: []CollectionNoteChange{}, RetainedOrder: []CollectionOrderEntry{}, Limitations: []string{
		"This compares exact declared selections and notes, not upstream changes, rights, asset bytes, or expected transfers. Acquisition still evaluates each source freshly.",
		"Removed selections do not remove holdings, notes, or sources. This comparison writes nothing.",
		"Hashes identify the manifest bytes read; the result does not freeze later edits. Review the acquisition preview before applying an updated collection.",
	}}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	beforePath, err := filepath.Abs(before)
	if err != nil {
		return empty, err
	}
	old, oldHash, err := readCollection(ctx, beforePath)
	if err != nil {
		return empty, fmt.Errorf("before collection: %w", err)
	}
	afterPath, err := filepath.Abs(after)
	if err != nil {
		return empty, err
	}
	next, nextHash, err := readCollection(ctx, afterPath)
	if err != nil {
		return empty, fmt.Errorf("after collection: %w", err)
	}
	r := empty
	r.Before = CollectionRevision{Path: beforePath, SHA256: oldHash, Name: old.Name, Items: len(old.Items)}
	r.After = CollectionRevision{Path: afterPath, SHA256: nextHash, Name: next.Name, Items: len(next.Items)}
	r.NameChanged = old.Name != next.Name
	oldItems, nextItems := collectionPositions(old), collectionPositions(next)
	var oldOrder, nextOrder []CollectionItem
	for i, item := range old.Items {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		key := collectionSelection(item)
		if _, ok := nextItems[key]; !ok {
			r.Removed = append(r.Removed, CollectionPosition{Position: i + 1, Item: item})
		} else {
			oldOrder = append(oldOrder, key)
		}
	}
	for i, item := range next.Items {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		key := collectionSelection(item)
		prior, ok := oldItems[key]
		if !ok {
			r.Added = append(r.Added, CollectionPosition{Position: i + 1, Item: item})
			continue
		}
		nextOrder = append(nextOrder, key)
		if prior.Item.Note != item.Note {
			r.NotesChanged = append(r.NotesChanged, CollectionNoteChange{Item: item, BeforeNote: prior.Item.Note, BeforePosition: prior.Position, AfterPosition: i + 1})
		}
	}
	if !slices.Equal(oldOrder, nextOrder) {
		for _, key := range nextOrder {
			prior, next := oldItems[key], nextItems[key]
			r.RetainedOrder = append(r.RetainedOrder, CollectionOrderEntry{Item: next.Item, BeforePosition: prior.Position, AfterPosition: next.Position})
		}
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	r.Changed = r.NameChanged || len(r.Added)+len(r.Removed)+len(r.NotesChanged)+len(r.RetainedOrder) > 0
	r.Complete = true
	return r, nil
}

func collectionSelection(item CollectionItem) CollectionItem {
	item.Note = ""
	return item
}

func collectionPositions(c Collection) map[CollectionItem]CollectionPosition {
	positions := make(map[CollectionItem]CollectionPosition, len(c.Items))
	for i, item := range c.Items {
		positions[collectionSelection(item)] = CollectionPosition{Position: i + 1, Item: item}
	}
	return positions
}
