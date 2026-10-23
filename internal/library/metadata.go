package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/blisspixel/nemalo/internal/textsafe"
)

const MaxHoldingMetadataBytes = 256 << 10

var ErrMetadataBudget = errors.New("selected holding metadata exceeds its output allowance")

// HoldingMetadata is recorded declaration evidence, not a check of stored bytes.
// Private intake paths and transfer URLs are deliberately absent.
type HoldingMetadata struct {
	Schema             string                       `json:"schema"`
	LibraryID          string                       `json:"library_id"`
	AssetID            string                       `json:"asset_id"`
	HoldingSHA256      string                       `json:"holding_sha256"`
	Bytes              int64                        `json:"recorded_bytes"`
	Format             string                       `json:"format"`
	RecordedStatus     string                       `json:"recorded_status"`
	ByteStatus         string                       `json:"byte_status"`
	Titles             []string                     `json:"titles"`
	Languages          []string                     `json:"languages"`
	IdentitiesRecorded bool                         `json:"identities_recorded"`
	IdentifiersOmitted int                          `json:"identifiers_omitted"`
	Identities         []Identity                   `json:"identities"`
	Sources            []MetadataSource             `json:"sources"`
	Recordings         []MetadataRecording          `json:"recordings"`
	UserDeclarations   []NarrationDeclarationStatus `json:"user_declarations"`
}

type MetadataSource struct {
	Kind             string           `json:"kind"`
	SourceID         string           `json:"source_id,omitempty"`
	SourceFile       string           `json:"source_file,omitempty"`
	DeclaredRights   []string         `json:"declared_rights,omitempty"`
	DeclaredLicenses []string         `json:"declared_licenses,omitempty"`
	Recording        *SourceRecording `json:"recording,omitempty"`
	Narration        *Narration       `json:"narration,omitempty"`
	Identities       []Identity       `json:"identities,omitempty"`
}

// MetadataRecording projects canonical relationships for the selected asset.
// Complete and byte identity remain journal evidence; no audio is opened.
type MetadataRecording struct {
	Selection          RecordingRef `json:"selection"`
	Positions          []int        `json:"positions"`
	DeclaredTracks     int          `json:"declared_tracks"`
	HeldTracks         int          `json:"held_tracks"`
	Missing            []int        `json:"missing_positions"`
	ByteIdentity       string       `json:"byte_identity,omitempty"`
	ByteIdentitySchema string       `json:"byte_identity_schema,omitempty"`
	ProviderNarration  *Narration   `json:"provider_narration,omitempty"`
	UserNarrations     []Narration  `json:"user_narrations"`
	Findings           []string     `json:"findings"`
}

// withManagedHolding is the single exact lookup used by content and metadata.
// The callback owns no write authority and runs while the reader lock is held.
func withManagedHolding(ctx context.Context, directory, assetID string, use func(*control, string, Holding, []Holding) error) (err error) {
	if !validAssetID(assetID) {
		return errors.New("invalid asset ID")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	state, err := LibraryStatus(ctx, abs)
	if err != nil || state.Status != "initialized" {
		if err == nil {
			err = errors.New("content library is not initialized")
		}
		return err
	}
	store, _, err := openControl(abs, false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.close()) }()
	_, lines, err := lockedManaged(store, state.LibraryID)
	if err != nil {
		return err
	}
	holding, ok := latestHoldings(lines)[assetID]
	if !ok {
		return errors.New("asset not present in managed holdings")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return use(store, abs, holding, lines)
}

// WithHoldingMetadata reads one bounded portable view, without opening bodies,
// receipts, providers or write state. The shared lock stays held during use.
// An error, including a close failure, invalidates any callback result.
func WithHoldingMetadata(ctx context.Context, directory, assetID string, use func(HoldingMetadata) error) error {
	if use == nil {
		return errors.New("metadata requires a reader callback")
	}
	return withManagedHolding(ctx, directory, assetID, func(store *control, abs string, holding Holding, lines []Holding) error {
		for _, name := range []string{journalName, operationsName, holdingsName, narrationName} {
			torn, err := checkedTornTail(store.root, name)
			if name == narrationName && errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || torn {
				return errors.Join(ErrStateReview, err)
			}
		}
		data, err := json.Marshal(holding)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		view := HoldingMetadata{Schema: "holding-metadata/1", LibraryID: holding.LibraryID, AssetID: holding.ID, HoldingSHA256: hex.EncodeToString(hash[:]), Bytes: holding.Bytes, Format: holding.Format, RecordedStatus: holding.Status, ByteStatus: "not_revalidated", Titles: holding.Titles, Languages: holding.Languages, IdentitiesRecorded: holding.IdentitiesRecorded, IdentifiersOmitted: holding.IdentifiersOmitted, Identities: holding.Identities, Sources: []MetadataSource{}, Recordings: []MetadataRecording{}, UserDeclarations: []NarrationDeclarationStatus{}}
		for _, source := range holding.Sources {
			if err := ctx.Err(); err != nil {
				return err
			}
			view.Sources = append(view.Sources, MetadataSource{Kind: source.Kind, SourceID: source.SourceID, SourceFile: source.SourceFile, DeclaredRights: source.DeclaredRights, DeclaredLicenses: source.DeclaredLicenses, Recording: source.Recording, Narration: source.Narration, Identities: source.Identities})
		}
		if holding.Format == "mp3" || holding.Format == "m4b" || holding.Format == "m4a" {
			r, err := recordingManifests(ctx, RecordingsResult{Library: abs, LibraryID: holding.LibraryID}, lines, "")
			if err != nil {
				return err
			}
			narrations, err := readNarrationJournal(store.root, holding.LibraryID)
			if err != nil {
				return err
			}
			if err := decorateUserNarrations(ctx, &r, narrations); err != nil {
				return err
			}
			if err := projectMetadataRecordings(ctx, &view, r); err != nil {
				return err
			}
		}
		encoded, err := textsafe.MarshalJSON(view, false)
		if err != nil {
			return err
		}
		if len(encoded) > MaxHoldingMetadataBytes {
			return ErrMetadataBudget
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return use(view)
	})
}

func recordingRefKey(ref RecordingRef) string {
	return narrationSelectionKey(NarrationSelection{SourceID: ref.SourceID, Format: ref.Format, Kind: ref.Kind})
}

func projectMetadataRecordings(ctx context.Context, view *HoldingMetadata, r RecordingsResult) error {
	identities := map[string]string{}
	for _, identity := range r.ByteIdentities {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, ref := range identity.Recordings {
			identities[recordingRefKey(ref)] = identity.ID
		}
	}
	users := map[string][]Narration{}
	for _, group := range r.UserNarrations {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, ref := range group.Recordings {
			key := recordingRefKey(ref)
			users[key] = append(users[key], group.Narration)
		}
	}
	selected := map[string]bool{}
	for _, recording := range r.Recordings {
		if err := ctx.Err(); err != nil {
			return err
		}
		positions := []int{}
		for _, track := range recording.Tracks {
			if track.AssetID == view.AssetID {
				positions = append(positions, track.Position)
			}
		}
		if len(positions) == 0 {
			continue
		}
		ref := RecordingRef{SourceID: recording.SourceID, Format: recording.Format, Kind: recording.Kind, Complete: recording.Complete}
		key := recordingRefKey(ref)
		selected[key] = true
		item := MetadataRecording{Selection: ref, Positions: positions, DeclaredTracks: recording.DeclaredTracks, HeldTracks: recording.HeldTracks, Missing: recording.Missing, ProviderNarration: recording.Narration, UserNarrations: []Narration{}, Findings: recording.Findings}
		if recording.Complete {
			item.ByteIdentity = identities[key]
			if item.ByteIdentity != "" {
				item.ByteIdentitySchema = RecordingByteIdentityVersion
			}
		}
		item.UserNarrations = append(item.UserNarrations, users[key]...)
		view.Recordings = append(view.Recordings, item)
	}
	for _, statement := range r.Declarations {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, selection := range statement.Declaration.Recordings {
			if selected[narrationSelectionKey(selection)] {
				view.UserDeclarations = append(view.UserDeclarations, statement)
				break
			}
		}
	}
	return ctx.Err()
}
