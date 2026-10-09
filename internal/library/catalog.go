// Package library owns portable byte-identity catalogs and preservation audits.
// Catalogs describe existing holdings, without publishing or deleting content.
package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/safeio"
)

const maxCatalogBytes = 16 << 20

type Location struct {
	Path          string `json:"path"`
	CandidateKind string `json:"candidate_kind"`
}

type Asset struct {
	ID        string             `json:"id"`
	SHA256    string             `json:"sha256"`
	Bytes     int64              `json:"bytes"`
	Locations []Location         `json:"locations"`
	Health    *assessment.Report `json:"health,omitempty"`
}

type Catalog struct {
	SchemaVersion int               `json:"schema_version"`
	ID            string            `json:"id"`
	CreatedAt     time.Time         `json:"created_at"`
	RootHint      string            `json:"root_hint"`
	Complete      bool              `json:"complete"`
	Assets        []Asset           `json:"assets"`
	Excluded      []inventory.Entry `json:"excluded"`
}

type Snapshot struct {
	Catalog    Catalog `json:"catalog"`
	Output     string  `json:"output,omitempty"`
	BytesRead  int64   `json:"bytes_read"`
	Durability string  `json:"durability"`
}

func validPath(name string) bool {
	return name != "." && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:\x00")
}
func validHash(hash string) bool {
	b, err := hex.DecodeString(hash)
	return err == nil && len(b) == 32 && strings.ToLower(hash) == hash
}

func (c Catalog) Validate() error {
	if c.SchemaVersion != 1 || !strings.HasPrefix(c.ID, "snapshot:") || len(c.ID) != 41 || c.CreatedAt.IsZero() || c.RootHint == "" || len(c.RootHint) > 4096 || !c.Complete || len(c.Assets)+len(c.Excluded) > 100000 {
		return errors.New("invalid or incomplete catalog header")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(c.ID, "snapshot:")); err != nil {
		return errors.New("invalid snapshot identifier")
	}
	paths, ids := map[string]bool{}, map[string]bool{}
	for _, a := range c.Assets {
		if !validHash(a.SHA256) || a.ID != "sha256:"+a.SHA256 || ids[a.ID] || a.Bytes < 0 || a.Bytes > 256<<20 || len(a.Locations) == 0 {
			return errors.New("invalid or duplicate catalog asset")
		}
		ids[a.ID] = true
		for _, l := range a.Locations {
			if !validPath(l.Path) || paths[l.Path] || !validKind(l.CandidateKind) {
				return errors.New("invalid or duplicate catalog location")
			}
			paths[l.Path] = true
			if len(paths) > 100000 {
				return errors.New("catalog location limit exceeded")
			}
		}
		if a.Health != nil && (a.Health.SHA256 != a.SHA256 || a.Health.Bytes != a.Bytes) {
			return errors.New("health evidence does not match asset identity")
		}
	}
	for _, e := range c.Excluded {
		if !validPath(e.Path) || paths[e.Path] || (e.Kind != "link" && e.Kind != "special") || e.Finding == "" {
			return errors.New("invalid excluded location")
		}
		paths[e.Path] = true
		if len(paths) > 100000 {
			return errors.New("catalog location limit exceeded")
		}
	}
	return nil
}

func validKind(kind string) bool {
	switch kind {
	case "ebook_candidate", "pdf_candidate", "audio_candidate", "archive_candidate", "recovery_data", "other":
		return true
	}
	return false
}

// Build uses the canonical inventory and assessment pipeline. It records exact
// duplicates as several locations of one byte asset, without merging editions.
func Build(ctx context.Context, directory string, limits inventory.Limits, assess bool) (Snapshot, error) {
	if limits.FileBytes > 256<<20 || limits.TotalBytes > 5<<30 {
		return Snapshot{}, errors.New("catalog limits must not exceed 256 MiB per file or 5 GiB per inventory pass")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	limits.Hashes = true
	abs, err := filepath.Abs(directory)
	if err != nil {
		return Snapshot{}, err
	}
	root, err := safeio.OpenRoot(abs)
	if err != nil {
		return Snapshot{}, err
	}
	defer root.Close()
	r, inventoryErr := inventory.InspectRoot(ctx, root, abs, limits)
	s := Snapshot{Catalog: Catalog{SchemaVersion: 1, ID: "snapshot:" + hex.EncodeToString(randomID()), CreatedAt: time.Now().UTC(), RootHint: r.Root, Complete: r.Complete, Assets: []Asset{}, Excluded: []inventory.Entry{}}, BytesRead: r.BytesRead, Durability: "not_saved"}
	positions := map[string]int{}
	for _, e := range r.Entries {
		if !validPath(e.Path) {
			s.Catalog.Complete = false
			inventoryErr = errors.Join(inventoryErr, fmt.Errorf("nonportable catalog path %q", e.Path))
			continue
		}
		if e.SHA256 == "" {
			s.Catalog.Excluded = append(s.Catalog.Excluded, e)
			continue
		}
		if i, ok := positions[e.SHA256]; ok {
			s.Catalog.Assets[i].Locations = append(s.Catalog.Assets[i].Locations, Location{e.Path, e.Kind})
			continue
		}
		positions[e.SHA256] = len(s.Catalog.Assets)
		s.Catalog.Assets = append(s.Catalog.Assets, Asset{ID: "sha256:" + e.SHA256, SHA256: e.SHA256, Bytes: e.Bytes, Locations: []Location{{e.Path, e.Kind}}})
	}
	if inventoryErr != nil {
		return s, inventoryErr
	}
	if assess {
		for i := range s.Catalog.Assets {
			a := &s.Catalog.Assets[i]
			name := ""
			for _, location := range a.Locations {
				ext := strings.ToLower(filepath.Ext(location.Path))
				if ext == ".epub" || ext == ".pdf" || ext == ".mp3" {
					name = location.Path
					break
				}
			}
			if name == "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				s.Catalog.Complete = false
				return s, err
			}
			h, err := assessment.CheckInRoot(ctx, root, name, assessment.Options{ExpectedBytes: a.Bytes, ExpectedSHA256: a.SHA256}, nil)
			s.BytesRead += h.Bytes
			if h.SHA256 != a.SHA256 || h.Bytes != a.Bytes || h.Status == "incomplete" {
				s.Catalog.Complete = false
				return s, fmt.Errorf("asset changed or could not be assessed %q: %w", name, err)
			}
			h.File = name
			a.Health = &h
		}
	}
	sort.Slice(s.Catalog.Assets, func(i, j int) bool { return s.Catalog.Assets[i].ID < s.Catalog.Assets[j].ID })
	return s, s.Catalog.Validate()
}

func randomID() []byte {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func Load(file string) (Catalog, error) {
	var c Catalog
	abs, err := filepath.Abs(file)
	if err != nil {
		return c, err
	}
	root, err := safeio.OpenResolvedRoot(filepath.Dir(abs))
	if err != nil {
		return c, err
	}
	defer root.Close()
	info, err := root.Lstat(filepath.Base(abs))
	if err != nil {
		return c, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxCatalogBytes {
		return c, errors.New("catalog must be a regular file at most 16 MiB")
	}
	f, err := safeio.OpenRegular(root, filepath.Base(abs), info, os.O_RDONLY)
	if err != nil {
		return c, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return c, err
	}
	if !os.SameFile(info, opened) {
		return c, errors.New("catalog changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCatalogBytes+1))
	if err != nil {
		return c, err
	}
	if len(data) > maxCatalogBytes {
		return c, errors.New("catalog exceeded 16 MiB")
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return c, errors.New("catalog contains trailing data")
	}
	return c, c.Validate()
}

// CheckDestination rejects existing outputs and source-contained destinations
// before expensive source reads. Save repeats this check before publication.
func CheckDestination(directory, file string) error {
	parent, _, err := openDestination(directory, file)
	if err == nil {
		err = parent.Close()
	}
	return err
}

// openDestination validates physical containment against the identities of the
// capabilities it returns. Save retains that parent through publication.
func openDestination(directory, file string) (parent *os.Root, abs string, err error) {
	abs, err = filepath.Abs(file)
	if err != nil {
		return nil, "", err
	}
	rootAbs, err := filepath.Abs(directory)
	if err != nil {
		return nil, "", err
	}
	sourceExpected, err := safeio.Stat(rootAbs)
	if err != nil {
		return nil, "", err
	}
	parentExpected, err := safeio.Stat(filepath.Dir(abs))
	if err != nil {
		return nil, "", err
	}
	physicalRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, "", err
	}
	physicalParent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return nil, "", err
	}
	source, err := safeio.OpenRoot(physicalRoot)
	if err != nil {
		return nil, "", err
	}
	defer source.Close()
	parent, err = safeio.OpenRoot(physicalParent)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		if err != nil {
			_ = parent.Close()
		}
	}()
	for _, bound := range []struct {
		before fs.FileInfo
		root   *os.Root
	}{{sourceExpected, source}, {parentExpected, parent}} {
		opened, statErr := bound.root.Stat(".")
		if statErr != nil || !os.SameFile(bound.before, opened) {
			return parent, "", errors.New("destination directory changed while opening")
		}
	}
	// Detect path replacement during physical-name resolution and acquisition.
	for _, bound := range []struct {
		name string
		root *os.Root
	}{{physicalRoot, source}, {physicalParent, parent}} {
		a, statErr := safeio.Stat(bound.name)
		b, rootErr := bound.root.Stat(".")
		if statErr != nil || rootErr != nil || !os.SameFile(a, b) {
			return parent, "", errors.New("destination relationship changed while opening")
		}
	}
	rel, relErr := filepath.Rel(physicalRoot, filepath.Join(physicalParent, filepath.Base(abs)))
	if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return parent, "", errors.New("catalog output must be outside the inventoried root")
	}
	if _, checkErr := parent.Lstat(filepath.Base(abs)); checkErr == nil {
		return parent, "", errors.New("catalog output already exists")
	} else if !errors.Is(checkErr, fs.ErrNotExist) {
		return parent, "", checkErr
	}
	return parent, abs, nil
}

// Save publishes a synced complete file through an exclusive hard link. A crash
// can leave a private pending file; it cannot replace an existing snapshot or
// expose partially written JSON at the requested destination. Hard-link support
// is required; there is no overwrite-prone fallback.
func Save(ctx context.Context, file string, s *Snapshot) (err error) {
	if err := s.Catalog.Validate(); err != nil {
		return err
	}
	parent, abs, err := openDestination(s.Catalog.RootHint, file)
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(abs)
	tmp := ".nemalo-snapshot-" + hex.EncodeToString(randomID()) + ".pending"
	f, err := parent.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, parent.Remove(tmp)) }()
	data, err := json.MarshalIndent(s.Catalog, "", "  ")
	if err != nil {
		_ = f.Close()
		return err
	}
	if len(data)+1 > maxCatalogBytes {
		_ = f.Close()
		return errors.New("catalog exceeds 16 MiB")
	}
	if err := ctx.Err(); err != nil {
		_ = f.Close()
		return err
	}
	_, writeErr := f.Write(append(data, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := parent.Link(tmp, name); err != nil {
		return fmt.Errorf("exclusive snapshot publication: %w", err)
	}
	s.Output = abs
	s.Durability = "file_synced; directory entry power-loss durability depends on filesystem"
	return nil
}
