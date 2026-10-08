package library

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/blisspixel/nemalo/internal/inventory"
)

type Finding struct {
	Path           string `json:"path"`
	Status         string `json:"status"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	ActualSHA256   string `json:"actual_sha256,omitempty"`
	Detail         string `json:"detail,omitempty"`
}

type Audit struct {
	SnapshotID string    `json:"snapshot_id"`
	Root       string    `json:"root"`
	Complete   bool      `json:"complete"`
	Unchanged  int       `json:"unchanged_locations"`
	BytesRead  int64     `json:"bytes_read"`
	Security   string    `json:"security_status"`
	Findings   []Finding `json:"findings"`
}

// Audit compares a fresh bounded inventory against an immutable baseline. Root
// is always explicit: a catalog's informational root hint grants no read access.
// Changed files and new files are reported, never repaired or removed.
func Verify(ctx context.Context, c Catalog, directory string, limits inventory.Limits) (Audit, error) {
	a := Audit{SnapshotID: c.ID, Findings: []Finding{}, Security: "not_scanned"}
	if err := c.Validate(); err != nil {
		return a, err
	}
	if directory == "" {
		return a, errors.New("audit requires an explicit root directory")
	}
	if limits.FileBytes > 256<<20 || limits.TotalBytes > 5<<30 {
		return a, errors.New("catalog limits must not exceed 256 MiB per file or 5 GiB per inventory pass")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	limits.Hashes = true
	r, inventoryErr := inventory.Inspect(ctx, directory, limits)
	a.Root, a.BytesRead = r.Root, r.BytesRead
	current := map[string]inventory.Entry{}
	for _, e := range r.Entries {
		current[e.Path] = e
	}
	for _, asset := range c.Assets {
		for _, location := range asset.Locations {
			e, exists := current[location.Path]
			delete(current, location.Path)
			f := Finding{Path: location.Path, ExpectedSHA256: asset.SHA256}
			switch {
			case !exists:
				f.Status = "missing"
				if inventoryErr != nil {
					f.Status = "not_observed_in_incomplete_inventory"
				}
			case e.SHA256 == "":
				f.Status = "unverified"
				f.Detail = e.Kind + ": " + e.Finding
			case e.SHA256 != asset.SHA256 || e.Bytes != asset.Bytes:
				f.Status = "changed"
				f.ActualSHA256 = e.SHA256
			default:
				a.Unchanged++
				continue
			}
			a.Findings = append(a.Findings, f)
		}
	}
	for _, excluded := range c.Excluded {
		e, exists := current[excluded.Path]
		delete(current, excluded.Path)
		if !exists || e.Kind != excluded.Kind || e.Finding != excluded.Finding {
			status := "excluded_location_changed"
			if !exists && inventoryErr != nil {
				status = "not_observed_in_incomplete_inventory"
			}
			a.Findings = append(a.Findings, Finding{Path: excluded.Path, Status: status, Detail: "previously " + excluded.Kind + ": " + excluded.Finding})
		}
	}
	// Inventory order is stable, so findings remain reproducible without map order.
	for _, e := range r.Entries {
		if _, ok := current[e.Path]; ok {
			a.Findings = append(a.Findings, Finding{Path: e.Path, Status: "added", ActualSHA256: e.SHA256, Detail: e.Kind + ": " + e.Finding})
		}
	}
	a.Complete = inventoryErr == nil
	if inventoryErr != nil {
		return a, inventoryErr
	}
	if len(a.Findings) > 0 {
		return a, errors.New("library differs from the catalog; source files unchanged")
	}
	return a, nil
}

type Page struct {
	SnapshotID string  `json:"snapshot_id"`
	Query      string  `json:"query"`
	Total      int     `json:"total_assets"`
	Offset     int     `json:"offset"`
	Assets     []Asset `json:"assets"`
}

// Find is a literal metadata filter, not a relevance or quality score.
func Find(c Catalog, query string, limit, offset int) (Page, error) {
	p := Page{SnapshotID: c.ID, Query: query, Offset: offset, Assets: []Asset{}}
	if err := c.Validate(); err != nil {
		return p, err
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 || len(query) > 1000 {
		return p, errors.New("holdings require limit 1-100, offset 0-100000, and query at most 1000 bytes")
	}
	query = strings.ToLower(strings.TrimSpace(query))
	for _, a := range c.Assets {
		values := []string{a.ID}
		for _, l := range a.Locations {
			values = append(values, l.Path, l.CandidateKind)
		}
		if a.Health != nil && a.Health.Checks.EPUB != nil {
			values = append(values, a.Health.Checks.EPUB.Titles...)
			values = append(values, a.Health.Checks.EPUB.Languages...)
		}
		match := query == ""
		for _, value := range values {
			if strings.Contains(strings.ToLower(value), query) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if p.Total >= offset && len(p.Assets) < limit {
			p.Assets = append(p.Assets, a)
		}
		p.Total++
	}
	return p, nil
}
