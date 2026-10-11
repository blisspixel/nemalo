package library

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// ManagedPage is one page of a managed library's current holdings, in the
// order each asset was first recorded. Matching is a literal, case-insensitive
// substring filter over recorded facts, not a relevance or quality score.
type ManagedPage struct {
	LibraryID string    `json:"library_id"`
	Library   string    `json:"library"`
	Query     string    `json:"query"`
	Format    string    `json:"format_filter"`
	Status    string    `json:"status_filter"`
	Total     int       `json:"total_holdings"`
	Offset    int       `json:"offset"`
	Holdings  []Holding `json:"holdings"`
}

// ValidStatusFilter accepts all, checked, or review.
func ValidStatusFilter(status string) bool {
	return status == "all" || status == "checked" || status == "review"
}

// FindManaged lists current holdings under the shared reader lock. It reads
// journals only; stored bytes are not opened or rechecked (use AuditManaged).
func FindManaged(ctx context.Context, directory, query, format, status string, limit, offset int) (ManagedPage, error) {
	p := ManagedPage{Query: query, Format: format, Status: status, Offset: offset, Holdings: []Holding{}}
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 || len(query) > 1000 {
		return p, errors.New("holdings require limit 1-100, offset 0-100000, and query at most 1000 bytes")
	}
	if !ValidFormatFilter(format) {
		return p, errors.New("format filter must be all, epub, pdf, or mp3")
	}
	if !ValidStatusFilter(status) {
		return p, errors.New("status filter must be all, checked, or review")
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return p, err
	}
	p.Library = root
	state, err := LibraryStatus(ctx, root)
	if err != nil {
		return p, err
	}
	if state.Status != "initialized" {
		return p, errors.New("holdings require an initialized library; run library init on this explicit directory")
	}
	p.LibraryID = state.LibraryID
	control, _, err := openControl(root, false)
	if err != nil {
		return p, err
	}
	defer control.close()
	_, lines, err := lockedManaged(control, state.LibraryID)
	if err != nil {
		return p, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	for _, h := range latestList(lines) {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		if (format != "all" && h.Format != format) || (status != "all" && h.Status != status) || !holdingMatches(h, query) {
			continue
		}
		if p.Total >= offset && len(p.Holdings) < limit {
			p.Holdings = append(p.Holdings, h)
		}
		p.Total++
	}
	return p, nil
}

func holdingMatches(h Holding, query string) bool {
	if query == "" {
		return true
	}
	values := []string{h.ID, h.Path, h.Format, h.Status, h.Reason}
	values = append(values, h.Titles...)
	values = append(values, h.Languages...)
	for _, id := range h.Identities {
		values = append(values, id.ID, id.Raw)
	}
	for _, s := range h.Sources {
		values = append(values, s.Path, s.SourceID, s.SourceFile)
		if s.Recording != nil {
			values = append(values, s.Recording.Format)
		}
	}
	for _, v := range values {
		if strings.Contains(strings.ToLower(v), query) {
			return true
		}
	}
	return false
}
