package discovery

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blisspixel/nemalo/internal/transfer"
)

// arXiv hosts. The query API and PDFs come from export.arxiv.org, the host
// arXiv names for programmatic access; the license comes from OAI-PMH.
const (
	ArxivExportHost = "export.arxiv.org"
	ArxivOAIHost    = "oaipmh.arxiv.org"
	// ArxivAccess marks an arXiv evaluation, whose single PDF offer has a
	// host-declared length and no checksum.
	ArxivAccess    = "arxiv_selected_version"
	ArxivPDFAccess = "arxiv_pdf_candidate"
)

// arXiv's terms allow one connection and one request every three seconds to
// its APIs; every arXiv request here shares that pace.
var arxivPace = 3 * time.Second

const maxArxivRecord = 1 << 20

var (
	arxivNew = regexp.MustCompile(`^[0-9]{4}\.[0-9]{4,5}$`)
	arxivOld = regexp.MustCompile(`^[a-z-]+(\.[A-Z]{2})?/[0-9]{7}$`)
	arxivVer = regexp.MustCompile(`v([1-9][0-9]{0,3})$`)
)

// SplitArxivID returns the paper identifier and version (0 when none) of
// arxiv:ID or arxiv:IDvN, in the new (2301.01234) or old (hep-th/9901001)
// scheme.
func SplitArxivID(id string) (paper string, version int, ok bool) {
	rest, ok := strings.CutPrefix(id, "arxiv:")
	if !ok || len(rest) > 64 {
		return "", 0, false
	}
	if m := arxivVer.FindStringSubmatchIndex(rest); m != nil {
		version, _ = strconv.Atoi(rest[m[2]:m[3]])
		rest = rest[:m[0]]
	}
	if arxivNew.MatchString(rest) {
		// YYMM.NNNN from April 2007 through 2014, YYMM.NNNNN since 2015.
		yymm, _ := strconv.Atoi(rest[:4])
		month, digits := yymm%100, len(rest)-5
		if month < 1 || month > 12 || yymm < 704 || (yymm < 1501) != (digits == 4) {
			return "", 0, false
		}
		return rest, version, true
	}
	if !arxivOld.MatchString(rest) {
		return "", 0, false
	}
	return rest, version, true
}

// ValidArxivID reports arxiv:ID or arxiv:IDvN.
func ValidArxivID(id string) bool {
	_, _, ok := SplitArxivID(id)
	return ok
}

// ArxivPDFName is the offered file name of one version: slashes of an old
// identifier become underscores.
func ArxivPDFName(paper string, version int) string {
	return strings.ReplaceAll(paper, "/", "_") + "v" + strconv.Itoa(version) + ".pdf"
}

// Arxiv evaluates and downloads one explicit arXiv paper version.
type Arxiv struct {
	Client *http.Client
	mu     sync.Mutex
	next   time.Time
}

func NewArxiv() *Arxiv { return &Arxiv{} }

func (a *Arxiv) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return NewFileClient(func(host string) bool { return host == ArxivExportHost || host == ArxivOAIHost })
}

// wait spaces every arXiv request by arxivPace.
func (a *Arxiv) wait(ctx context.Context) error {
	a.mu.Lock()
	delay := time.Until(a.next)
	if delay < 0 {
		delay = 0
	}
	a.next = time.Now().Add(delay + arxivPace)
	a.mu.Unlock()
	if delay == 0 {
		return ctx.Err()
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// get sends one request at arXiv's pace. A transport failure or a 429 or
// 5xx answer before any body is tried again, at most three times in all,
// each at the same pace and after any Retry-After of at most 30 seconds.
func (a *Arxiv) get(ctx context.Context, method, raw string) (*http.Response, error) {
	client := a.client()
	for attempt := 1; ; attempt++ {
		if err := a.wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
		req.Header.Set("Accept-Encoding", "identity")
		resp, err := client.Do(req)
		retry := false
		switch {
		case err != nil:
			retry = transfer.Transient(err) && ctx.Err() == nil
		case resp.StatusCode == 429 || resp.StatusCode >= 500:
			wait := time.Until(retryUntil(resp.Header.Get("Retry-After"), time.Now()))
			if retry = wait <= maxRetryWait; retry && wait > 0 {
				a.mu.Lock()
				a.next = time.Now().Add(wait)
				a.mu.Unlock()
			}
		}
		if !retry || attempt >= downloadAttempts {
			return resp, err
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
		}
	}
}

func (a *Arxiv) record(ctx context.Context, raw string) ([]byte, error) {
	resp, err := a.get(ctx, http.MethodGet, raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arXiv returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArxivRecord+1))
	if err != nil {
		return nil, fmt.Errorf("arXiv record stopped partway: %w", err)
	}
	if len(body) == 0 || len(body) > maxArxivRecord {
		return nil, errors.New("arXiv record is empty or exceeds 1 MiB")
	}
	return body, nil
}

type arxivFeed struct {
	Total   int          `xml:"http://a9.com/-/spec/opensearch/1.1/ totalResults"`
	Entries []arxivEntry `xml:"http://www.w3.org/2005/Atom entry"`
}

type arxivEntry struct {
	ID        string `xml:"http://www.w3.org/2005/Atom id"`
	Title     string `xml:"http://www.w3.org/2005/Atom title"`
	Summary   string `xml:"http://www.w3.org/2005/Atom summary"`
	Published string `xml:"http://www.w3.org/2005/Atom published"`
	Updated   string `xml:"http://www.w3.org/2005/Atom updated"`
	Authors   []struct {
		Name string `xml:"http://www.w3.org/2005/Atom name"`
	} `xml:"http://www.w3.org/2005/Atom author"`
	DOI     string `xml:"http://arxiv.org/schemas/atom doi"`
	Comment string `xml:"http://arxiv.org/schemas/atom comment"`
	Primary struct {
		Term string `xml:"term,attr"`
	} `xml:"http://arxiv.org/schemas/atom primary_category"`
}

// parseArxivFeed reads the one entry the query API returns for paper, and the
// version its identifier names.
func parseArxivFeed(body []byte, paper string) (arxivEntry, int, error) {
	var feed arxivFeed
	d := xml.NewDecoder(strings.NewReader(string(body)))
	d.Strict, d.Entity = true, map[string]string{}
	if err := d.Decode(&feed); err != nil {
		return arxivEntry{}, 0, errors.New("arXiv returned a record that is not a readable Atom feed")
	}
	if feed.Total == 0 || len(feed.Entries) == 0 {
		return arxivEntry{}, 0, errors.New("arXiv has no paper with that identifier")
	}
	if len(feed.Entries) != 1 {
		return arxivEntry{}, 0, errors.New("arXiv returned more than one paper")
	}
	e := feed.Entries[0]
	abs, ok := strings.CutPrefix(strings.TrimSpace(e.ID), "http://arxiv.org/abs/")
	if !ok {
		abs, ok = strings.CutPrefix(strings.TrimSpace(e.ID), "https://arxiv.org/abs/")
	}
	got, version, valid := SplitArxivID("arxiv:" + abs)
	if !ok || !valid || got != paper || version == 0 {
		return arxivEntry{}, 0, errors.New("arXiv returned another paper or no version")
	}
	return e, version, nil
}

// oaiRecord is an OAI-PMH GetRecord answer in the arXiv metadata format; its
// arXiv element is in its own namespace.
type oaiRecord struct {
	Error struct {
		Code string `xml:"code,attr"`
	} `xml:"http://www.openarchives.org/OAI/2.0/ error"`
	GetRecord struct {
		Record struct {
			Metadata struct {
				ArXiv struct {
					ID      string `xml:"http://arxiv.org/OAI/arXiv/ id"`
					License string `xml:"http://arxiv.org/OAI/arXiv/ license"`
				} `xml:"http://arxiv.org/OAI/arXiv/ arXiv"`
			} `xml:"http://www.openarchives.org/OAI/2.0/ metadata"`
		} `xml:"http://www.openarchives.org/OAI/2.0/ record"`
	} `xml:"http://www.openarchives.org/OAI/2.0/ GetRecord"`
}

// Evaluate reads one paper: the query API names its version (the latest for
// an unversioned request), OAI-PMH its license, which is the latest
// version's, and a HEAD request the PDF's length. Nothing is downloaded.
func (a *Arxiv) Evaluate(ctx context.Context, id string) (Evaluation, error) {
	e := Evaluation{Source: "arxiv", Access: ArxivAccess, Restrictions: map[string]string{}, Files: []OfferedFile{}, Limitations: []string{
		"arXiv publishes no checksum or size for a PDF. Its length is the one the host declares when asked, and the transfer checks it; the SHA-256 Nemalo measures identifies the bytes it received.",
		"The license is arXiv's record for the latest version. It is applied only when the selected version is that one.",
		"A preprint, its later versions, and its published article are distinct works of record, not one file.",
	}}
	paper, version, ok := SplitArxivID(id)
	if !ok {
		return e, errors.New("evaluate requires arxiv:ID or arxiv:IDvN, such as arxiv:1706.03762v7")
	}
	query := func(v int) (arxivEntry, int, error) {
		target := paper
		if v > 0 {
			target += "v" + strconv.Itoa(v)
		}
		u := "https://" + ArxivExportHost + "/api/query?" + url.Values{"id_list": {target}, "max_results": {"1"}}.Encode()
		body, err := a.record(ctx, u)
		if err != nil {
			return arxivEntry{}, 0, err
		}
		e.MetadataSHA256 = digest(body)
		e.MetadataURL = u
		return parseArxivFeed(body, paper)
	}
	latestEntry, latest, err := query(0)
	if err != nil {
		return e, err
	}
	entry := latestEntry
	if version == 0 {
		version = latest
	} else if version != latest {
		if version > latest {
			return e, fmt.Errorf("arXiv has no version %d of %s; the latest is %d", version, paper, latest)
		}
		if entry, _, err = query(version); err != nil {
			return e, err
		}
	}
	e.ID = "arxiv:" + paper + "v" + strconv.Itoa(version)
	e.Title = cleanList([]string{entry.Title}, 500)
	for _, author := range entry.Authors {
		if name := textField(author.Name, 200); name != "" && len(e.Creators) < 100 {
			e.Creators = append(e.Creators, name)
		}
	}
	e.Dates = cleanList([]string{"published " + entry.Published, "version updated " + entry.Updated}, 100)
	if doi := textField(entry.DOI, 200); doi != "" {
		e.Publishers = append(e.Publishers, "published as doi:"+doi)
	}
	if subject := textField(entry.Primary.Term, 50); subject != "" {
		e.Subjects = []string{subject}
	}
	e.LandingPage = "https://arxiv.org/abs/" + paper + "v" + strconv.Itoa(version)
	e.Dates = append(e.Dates, fmt.Sprintf("version %d of %d", version, latest))
	if strings.Contains(strings.ToLower(entry.Comment), "withdrawn") {
		e.Restrictions["comment"] = "the version's comment mentions withdrawal: " + textField(entry.Comment, 300)
	}
	// The license of the latest version; another version's is unknown.
	oai := "https://" + ArxivOAIHost + "/oai?" + url.Values{"verb": {"GetRecord"}, "identifier": {"oai:arXiv.org:" + paper}, "metadataPrefix": {"arXiv"}}.Encode()
	if body, err := a.record(ctx, oai); err == nil {
		var rec oaiRecord
		d := xml.NewDecoder(strings.NewReader(string(body)))
		d.Strict, d.Entity = true, map[string]string{}
		decodeErr := d.Decode(&rec)
		arxiv := rec.GetRecord.Record.Metadata.ArXiv
		if decodeErr != nil || rec.Error.Code != "" || strings.TrimSpace(arxiv.ID) != paper {
			e.Rights = append(e.Rights, "arXiv's license record did not name this paper; its license is not established.")
		} else {
			license := strings.TrimSpace(arxiv.License)
			switch {
			case license == "":
				e.Rights = append(e.Rights, "arXiv records no license for this paper.")
			case version != latest:
				e.Rights = append(e.Rights, fmt.Sprintf("The latest version, %d, is under %s; the license of version %d is not established.", latest, textField(license, 300), version))
			case strings.HasPrefix(license, "http://") || strings.HasPrefix(license, "https://"):
				e.LicenseURLs = append(e.LicenseURLs, textField(license, 300))
			default:
				e.Rights = append(e.Rights, "arXiv's license record: "+textField(license, 300))
			}
		}
	} else if ctx.Err() != nil {
		return e, ctx.Err()
	} else {
		e.Rights = append(e.Rights, "arXiv's license record could not be read: "+textField(err.Error(), 200))
	}
	file := OfferedFile{Name: ArxivPDFName(paper, version), Format: "PDF", CandidateKind: "pdf_candidate", Access: ArxivPDFAccess, PrivateState: "not_declared", RestrictionState: "not_declared", RightsStatus: "not_resolved_at_file_level", DRMStatus: "not_assessed", DownloadURL: arxivPDFURL(paper, version), Findings: []string{}}
	// The host's declared length, from a HEAD request, is the size checked.
	resp, err := a.get(ctx, http.MethodHead, file.DownloadURL)
	switch {
	case err != nil:
		if ctx.Err() != nil {
			return e, ctx.Err()
		}
		file.Findings = append(file.Findings, "the PDF's length could not be read: "+textField(err.Error(), 200))
	default:
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && resp.ContentLength > 0 && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/pdf") {
			n := resp.ContentLength
			file.Bytes = &n
		} else {
			file.Findings = append(file.Findings, fmt.Sprintf("arXiv answered HTTP %d with no PDF length", resp.StatusCode))
		}
	}
	e.Files = append(e.Files, file)
	e.CheckedAt = time.Now().UTC()
	e.Complete = len(e.Title) > 0
	return e, nil
}

func arxivPDFURL(paper string, version int) string {
	return "https://" + ArxivExportHost + "/pdf/" + paper + "v" + strconv.Itoa(version)
}

// Download fetches the selected version's PDF from export.arxiv.org only.
func (a *Arxiv) Download(ctx context.Context, file OfferedFile) (*http.Response, error) {
	paper, version, ok := splitArxivPDFName(file.Name)
	if !ok || file.Access != ArxivPDFAccess || file.DownloadURL != arxivPDFURL(paper, version) {
		return nil, errors.New("download URL does not match the selected arXiv version")
	}
	return a.get(ctx, http.MethodGet, file.DownloadURL)
}

// splitArxivPDFName reverses ArxivPDFName for either identifier scheme.
func splitArxivPDFName(name string) (string, int, bool) {
	stem, ok := strings.CutSuffix(name, ".pdf")
	if !ok {
		return "", 0, false
	}
	if i := strings.Index(stem, "_"); i > 0 {
		stem = stem[:i] + "/" + stem[i+1:]
	}
	paper, version, ok := SplitArxivID("arxiv:" + stem)
	return paper, version, ok && version > 0 && ArxivPDFName(paper, version) == name
}

func cleanList(values []string, limit int) []string {
	out := []string{}
	for _, v := range values {
		if v = textField(v, limit); v != "" {
			out = append(out, v)
		}
	}
	return out
}
