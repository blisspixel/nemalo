// Package collection acquires explicitly curated validation fixtures into a separate intake.
// It does not publish a checked library or implement the production transfer manager.
package collection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"time"

	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/safeio"
	"github.com/blisspixel/nemalo/internal/transfer"
)

type Asset struct {
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	Format       string   `json:"format"`
	Title        string   `json:"title,omitempty"`
	Contributors []string `json:"contributors,omitempty"`
}

type Resource struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Authors      []string `json:"authors"`
	Languages    []string `json:"languages"`
	Type         string   `json:"type"`
	Rationale    string   `json:"rationale"`
	LandingURL   string   `json:"landing_url"`
	MetadataURL  string   `json:"metadata_url"`
	Rights       string   `json:"rights"`
	RightsURL    string   `json:"rights_url"`
	ReviewStatus string   `json:"review_status"`
	Notes        string   `json:"notes,omitempty"`
	Assets       []Asset  `json:"assets"`
}

type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	Title         string     `json:"title"`
	Resources     []Resource `json:"resources"`
}

type Receipt struct {
	ResourceID string            `json:"resource_id"`
	Asset      Asset             `json:"asset"`
	Bytes      int64             `json:"bytes"`
	SHA256     string            `json:"sha256"`
	AcquiredAt time.Time         `json:"acquired_at"`
	FinalURL   string            `json:"final_url"`
	Checks     assessment.Checks `json:"checks"`
	Antivirus  string            `json:"antivirus"`
}

type Result struct {
	ResourceID string    `json:"resource_id"`
	Acquired   bool      `json:"acquired"`
	Assets     []Receipt `json:"assets"`
	Errors     []string  `json:"errors,omitempty"`
}

type Report struct {
	SchemaVersion     int       `json:"schema_version"`
	GeneratedAt       time.Time `json:"generated_at"`
	Complete          bool      `json:"complete"`
	AcquiredResources int       `json:"acquired_resources"`
	StoredBytes       int64     `json:"stored_bytes"`
	TransferredBytes  int64     `json:"transferred_bytes"`
	SecurityStatus    string    `json:"security_status"`
	Results           []Result  `json:"results"`
}

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,100}$`)

func Load(r io.Reader) (Manifest, error) {
	var m Manifest
	data, err := io.ReadAll(io.LimitReader(r, (2<<20)+1))
	if err != nil {
		return m, err
	}
	if len(data) > 2<<20 {
		return m, errors.New("manifest exceeds 2 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return m, errors.New("manifest must contain one JSON object")
	}
	return m, m.Validate()
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != 1 || len(m.Resources) == 0 || len(m.Resources) > 1000 {
		return errors.New("invalid manifest schema or resource count")
	}
	seen := map[string]bool{}
	for _, r := range m.Resources {
		if !identifier.MatchString(r.ID) || seen[r.ID] || r.Title == "" || r.Rationale == "" || r.Rights == "" || r.RightsURL == "" || len(r.Languages) == 0 || len(r.Assets) == 0 || len(r.Assets) > 500 {
			return fmt.Errorf("invalid or duplicate resource %q", r.ID)
		}
		seen[r.ID] = true
		if r.Type != "ebook" && r.Type != "audiobook" && r.Type != "article" {
			return fmt.Errorf("invalid resource type %q", r.Type)
		}
		names := map[string]bool{}
		for _, a := range r.Assets {
			if !identifier.MatchString(a.Name) || a.Name == "." || a.Name == ".." || names[a.Name] {
				return fmt.Errorf("invalid asset name %q", a.Name)
			}
			names[a.Name] = true
			if a.Format != "epub" && a.Format != "pdf" && a.Format != "mp3" {
				return fmt.Errorf("unsupported format %q", a.Format)
			}
			if path.Ext(a.Name) != "."+a.Format {
				return errors.New("asset extension and format differ")
			}
			if err := permittedURL(a.URL); err != nil {
				return err
			}
		}
	}
	return nil
}

func permittedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return fmt.Errorf("disallowed download URL %q", raw)
	}
	h := u.Host
	if !permittedHost(h) {
		return fmt.Errorf("download host %q is outside curated providers", h)
	}
	return nil
}

func NewClient() *http.Client {
	return discovery.NewFileClient(permittedHost)
}

func permittedHost(host string) bool {
	return discovery.ArchiveDownloadHost(host) || host == "gutenberg.pglaf.org" || host == "mirror.cs.odu.edu" || host == "export.arxiv.org" || host == "arxiv.org"
}

type Acquirer struct {
	Client      *http.Client
	Budget      int64
	Interval    time.Duration
	nextRequest time.Time
}

// Acquire never overwrites an unreceipted asset. Existing receipted files are
// rehashed and reinspected, not silently accepted because they already exist.
func (a Acquirer) Acquire(ctx context.Context, m Manifest, directory string, progress io.Writer) (Report, error) {
	report := Report{SchemaVersion: 1, GeneratedAt: time.Now().UTC(), SecurityStatus: "not_scanned"}
	if err := m.Validate(); err != nil {
		return report, err
	}
	if a.Budget <= 0 || a.Budget > 5<<30 || a.Interval < 0 {
		return report, errors.New("invalid collection budget or interval")
	}
	if a.Client == nil {
		a.Client = NewClient()
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return report, err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return report, errors.New("collection root must be a real directory")
	}
	root, err := safeio.OpenRoot(directory)
	if err != nil {
		return report, err
	}
	defer root.Close()
	lock, err := root.OpenFile(".collection.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return report, fmt.Errorf("collection writer lock: %w", err)
	}
	defer func() { _ = lock.Close(); _ = root.Remove(".collection.lock") }()
	remaining := a.Budget
	for _, resource := range m.Resources {
		result := Result{ResourceID: resource.ID, Acquired: true}
		if err := root.MkdirAll(resource.ID, 0700); err != nil {
			return report, err
		}
		for _, asset := range resource.Assets {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			receipt, transferred, err := a.acquireAsset(ctx, root, resource.ID, asset, remaining)
			report.TransferredBytes += transferred
			remaining -= transferred
			if err != nil {
				result.Acquired = false
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", asset.Name, err))
			} else {
				result.Assets = append(result.Assets, receipt)
				report.StoredBytes += receipt.Bytes
			}
			if progress != nil {
				_, _ = fmt.Fprintf(progress, "%s/%s: acquired=%t\n", resource.ID, asset.Name, err == nil)
				if err != nil {
					_, _ = fmt.Fprintf(progress, "  error: %q\n", err.Error())
				}
			}
			if err := ctx.Err(); err != nil {
				result.Acquired = false
				report.Results = append(report.Results, result)
				return report, err
			}
		}
		if result.Acquired {
			report.AcquiredResources++
		}
		report.Results = append(report.Results, result)
	}
	report.Complete = report.AcquiredResources == len(m.Resources)
	if !report.Complete {
		return report, errors.New("collection is incomplete; inspect per-asset errors")
	}
	return report, nil
}

func (a *Acquirer) acquireAsset(ctx context.Context, root *os.Root, id string, asset Asset, remaining int64) (Receipt, int64, error) {
	name := id + "/" + asset.Name
	receipt := Receipt{ResourceID: id, Asset: asset, Antivirus: "not_scanned"}
	if info, err := root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return receipt, 0, errors.New("existing asset is not a regular file")
		}
		info, err := root.Lstat(name + ".receipt.json")
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<10 {
			return receipt, 0, errors.New("existing file has no regular bounded receipt; refusing overwrite")
		}
		receiptFile, err := safeio.OpenRegular(root, name+".receipt.json", info, os.O_RDONLY)
		if err != nil {
			return receipt, 0, errors.New("existing file has no receipt; refusing overwrite")
		}
		data, err := io.ReadAll(io.LimitReader(receiptFile, (64<<10)+1))
		closeErr := receiptFile.Close()
		if err != nil || closeErr != nil || len(data) > 64<<10 {
			return receipt, 0, errors.New("existing file has no bounded receipt; refusing overwrite")
		}
		var old Receipt
		if err := json.Unmarshal(data, &old); err != nil || old.Asset.URL != asset.URL || old.Asset.Name != asset.Name || old.Asset.Format != asset.Format || old.ResourceID != id || old.Bytes <= 0 || old.Bytes > 256<<20 {
			return receipt, 0, errors.New("existing receipt does not match selection")
		}
		assetInfo, err := root.Lstat(name)
		if err != nil {
			return receipt, 0, err
		}
		f, err := safeio.OpenRegular(root, name, assetInfo, os.O_RDONLY)
		if err != nil {
			return receipt, 0, err
		}
		defer f.Close()
		info, err = f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != old.Bytes {
			return receipt, 0, errors.New("existing asset size or kind changed")
		}
		h := sha256.New()
		_, err = io.Copy(h, io.LimitReader(f, old.Bytes+1))
		if err != nil || hex.EncodeToString(h.Sum(nil)) != old.SHA256 {
			return receipt, 0, errors.New("existing asset hash changed")
		}
		old.Checks, err = assessment.InspectContext(ctx, f, old.Bytes, asset.Format)
		old.Asset = asset
		return old, 0, err
	} else if !os.IsNotExist(err) {
		return receipt, 0, err
	}
	if remaining <= 0 {
		return receipt, 0, errors.New("transfer budget exhausted")
	}
	limit := min(remaining, int64(256<<20))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return receipt, 0, err
	}
	req.Header.Set("User-Agent", "nemalo-validation/0.1 (+https://github.com/blisspixel/nemalo)")
	if wait := time.Until(a.nextRequest); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return receipt, 0, ctx.Err()
		case <-timer.C:
		}
	}
	a.nextRequest = time.Now().Add(a.Interval)
	res, err := a.Client.Do(req)
	if err != nil {
		return receipt, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return receipt, 0, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if res.ContentLength > limit {
		return receipt, 0, errors.New("declared size exceeds transfer limit")
	}
	f, err := root.OpenFile(name+".partial", os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return receipt, 0, err
	}
	defer func() { _ = f.Close(); _ = root.Remove(name + ".partial") }()
	stream, err := transfer.Copy(ctx, f, res.Body, limit, transfer.Expectation{Bytes: res.ContentLength})
	n := stream.Bytes
	if err != nil {
		return receipt, n, err
	}
	receipt.Bytes, receipt.SHA256 = n, stream.SHA256
	receipt.AcquiredAt, receipt.FinalURL = time.Now().UTC(), res.Request.URL.String()
	receipt.Checks, err = assessment.InspectContext(ctx, f, n, asset.Format)
	if err != nil {
		return receipt, n, err
	}
	if err := f.Sync(); err != nil {
		return receipt, n, err
	}
	if err := f.Close(); err != nil {
		return receipt, n, err
	}
	if err := root.Rename(name+".partial", name); err != nil {
		return receipt, n, err
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return receipt, n, err
	}
	out, err := root.OpenFile(name+".receipt.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return receipt, n, err
	}
	_, writeErr := out.Write(append(data, '\n'))
	syncErr, closeErr := out.Sync(), out.Close()
	return receipt, n, errors.Join(writeErr, syncErr, closeErr)
}
