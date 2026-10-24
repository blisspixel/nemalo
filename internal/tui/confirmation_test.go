package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
)

func TestWriteReviewsIgnoreCommittedTextThatSpellsControlKeys(t *testing.T) {
	for _, kind := range []string{"collection", "file", "recording", "init", "import", "rescan", "cleanup", "restore", "purge"} {
		for _, text := range []string{"enter", "esc", "ctrl+c", "pgdown", "f7", "ctrl+r"} {
			t.Run(kind+"/"+text, func(t *testing.T) {
				m := newModel(context.Background(), app.Service{Providers: map[string]app.Searcher{}, Scanner: contentsNoScanner{t}})
				root := t.TempDir()
				switch kind {
				case "collection":
					m.mode = "collections"
					m.collectionReview = &collectionReview{request: app.CollectionAcquireRequest{Library: root, Collection: "selection.json", Intake: "intake", Apply: true, Plan: "reviewed"}}
				case "file":
					m.downloadReview = &acquisition.Request{SourceID: "archive:one", File: "one.epub", Output: root}
				case "recording":
					m.recordingReview = &acquisition.RecordingRequest{SourceID: "archive:one", Format: "MP3", Output: root}
				default:
					m.mode = "state"
					m.confirmKind, m.confirmRoot, m.confirmSource = kind, root, "selected-source"
					m.cleanupDigest, m.purgeDigest = "reviewed", "reviewed"
				}
				collection, file, recording := m.collectionReview, m.downloadReview, m.recordingReview
				_, cmd := m.Update(tea.KeyPressMsg{Code: []rune(text)[0], Text: text})
				if cmd != nil || m.busy || m.collectionReview != collection || m.downloadReview != file || m.recordingReview != recording || (kind != "collection" && kind != "file" && kind != "recording" && m.confirmRoot != root) {
					t.Fatal("committed text became a write-review control", kind, text, m.busy, m.confirmRoot)
				}
			})
		}
	}
}
