package discovery

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	PMCHost      = "pmc-oa-opendata.s3.amazonaws.com"
	PMCAccess    = "pmc_selected_version"
	PMCPDFAccess = "pmc_pdf_candidate"
	pmcPace      = time.Second
	maxPMCRecord = 1 << 20
)

var pmcID = regexp.MustCompile(`^pmc:(PMC[1-9][0-9]{0,9})(?:\.([1-9][0-9]{0,3}))?$`)

// SplitPMCID accepts a canonical accession and optional explicit version.
func SplitPMCID(id string) (string, int, bool) {
	m := pmcID.FindStringSubmatch(id)
	if m == nil {
		return "", 0, false
	}
	v, _ := strconv.Atoi(m[2])
	return m[1], v, true
}
func ValidPMCID(id string) bool { _, _, ok := SplitPMCID(id); return ok }
func PMCPDFName(paper string, version int) string {
	return paper + "." + strconv.Itoa(version) + ".pdf"
}
func pmcPDFURL(paper string, version int) string {
	return "https://" + PMCHost + "/" + paper + "." + strconv.Itoa(version) + "/" + PMCPDFName(paper, version)
}

// PMC uses one checked connection and a shared request pace. Client is injectable
// for offline tests; production callers use NewPMC and cannot choose endpoints.
type PMC struct {
	Client *http.Client
	requestPacer
}

func NewPMC() *PMC { p := &PMC{}; p.Client = p.newClient(); return p }
func (p *PMC) newClient() *http.Client {
	c := NewFileClient(func(host string) bool { return host == PMCHost })
	c.Transport.(fileTransport).base.(*http.Transport).MaxConnsPerHost = 1
	// Objects have canonical URLs. Refuse redirects, including same-host key
	// changes, rather than fetching a different object under the selected name.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("PMC redirects are not supported") }
	return c
}
func (p *PMC) client() *http.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Client == nil {
		p.Client = p.newClient()
	}
	return p.Client
}
func (p *PMC) record(ctx context.Context, raw string) ([]byte, error) {
	resp, err := p.requestPacer.get(ctx, p.client(), pmcPace, http.MethodGet, raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PMC returned HTTP %d", resp.StatusCode)
	}
	want := "application/xml"
	if strings.HasSuffix(raw, ".json") {
		want = "application/json"
	}
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	// PMC's metadata objects currently use binary/octet-stream (observed
	// 2026-10-25). Accept it only for pinned JSON objects, which are still
	// parsed strictly; listings must identify themselves as XML.
	if err != nil || (media != want && !(want == "application/json" && media == "binary/octet-stream")) {
		return nil, errors.New("PMC returned an unexpected metadata content type")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPMCRecord+1))
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || len(b) > maxPMCRecord {
		return nil, errors.New("PMC record is empty or exceeds 1 MiB")
	}
	return b, nil
}

type pmcListing struct {
	XMLName   xml.Name `xml:"http://s3.amazonaws.com/doc/2006-03-01/ ListBucketResult"`
	Name      string   `xml:"Name"`
	Prefix    string   `xml:"Prefix"`
	Truncated *bool    `xml:"IsTruncated"`
	Prefixes  []struct {
		Prefix string `xml:"Prefix"`
	} `xml:"CommonPrefixes"`
	Objects []struct {
		Key  string `xml:"Key"`
		Size int64  `xml:"Size"`
	} `xml:"Contents"`
}

func (p *PMC) list(ctx context.Context, prefix, delimiter string) (pmcListing, error) {
	q := url.Values{"list-type": {"2"}, "prefix": {prefix}, "max-keys": {"1000"}}
	if delimiter != "" {
		q.Set("delimiter", delimiter)
	}
	b, err := p.record(ctx, "https://"+PMCHost+"/?"+q.Encode())
	if err != nil {
		return pmcListing{}, err
	}
	var l pmcListing
	if err = xml.Unmarshal(b, &l); err != nil {
		return l, errors.New("PMC returned an invalid object listing")
	}
	if l.Name != "pmc-oa-opendata" || l.Prefix != prefix || l.Truncated == nil || *l.Truncated || len(l.Prefixes)+len(l.Objects) > 1000 {
		return l, errors.New("PMC object listing is mismatched, incomplete or over budget")
	}
	return l, nil
}

type pmcMetadata struct {
	PMCID         string `json:"pmcid"`
	Version       int    `json:"version"`
	PMID          *int64 `json:"pmid"`
	DOI           string `json:"doi"`
	Title         string `json:"title"`
	Citation      string `json:"citation"`
	OpenAccess    *bool  `json:"is_pmc_openaccess"`
	Manuscript    *bool  `json:"is_manuscript"`
	HistoricalOCR *bool  `json:"is_historical_ocr"`
	Retracted     *bool  `json:"is_retracted"`
	License       string `json:"license_code"`
	PDFURL        string `json:"pdf_url"`
}

// Reject duplicate or case-variant keys before decoding typed fields. Unknown
// fields remain forward-compatible, but cannot override a canonical declaration.
func parsePMCMetadata(body []byte) (pmcMetadata, error) {
	var m pmcMetadata
	d := json.NewDecoder(strings.NewReader(string(body)))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return m, errors.New("PMC metadata is not an object")
	}
	seen := map[string]bool{}
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			return m, err
		}
		key, ok := tok.(string)
		if !ok || key != strings.ToLower(key) || seen[key] {
			return m, errors.New("PMC metadata has ambiguous field names")
		}
		seen[key] = true
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return m, err
		}
	}
	// Unmarshal also rejects malformed closing tokens and trailing JSON values.
	err = json.Unmarshal(body, &m)
	return m, err
}

func pmcFlag(b *bool) string {
	if b == nil {
		return "not_declared"
	}
	return strconv.FormatBool(*b)
}
func pmcCC(license string) bool {
	switch license {
	case "CC0", "CC BY", "CC BY-SA", "CC BY-ND", "CC BY-NC", "CC BY-NC-SA", "CC BY-NC-ND":
		return true
	}
	return false
}

// Evaluate resolves only this accession's bounded complete version list. License
// and flags belong to the selected version, never to another available version.
func (p *PMC) Evaluate(ctx context.Context, id string) (Evaluation, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	e := Evaluation{Source: "pmc", Access: PMCAccess, Files: []OfferedFile{}, Restrictions: map[string]string{}, Limitations: []string{
		"Source: NIH NLM NCBI PubMed Central (PMC) Article Datasets. NLM does not endorse Nemalo.",
		"Article-version objects can change or be removed. These observations do not necessarily reflect the most current data available from NLM. MD5 checks transport integrity; measured SHA-256 identifies the acquired bytes.",
		"Licenses and article flags are provider declarations, not scientific endorsement, safety or semantic completeness. Only known Creative Commons declarations in the open-access subset are offered for acquisition; redistribution remains subject to the article's terms.",
	}}
	paper, version, ok := SplitPMCID(id)
	if !ok {
		return e, errors.New("evaluate requires pmc:PMCnumber or pmc:PMCnumber.version")
	}
	l, err := p.list(ctx, paper+".", "/")
	if err != nil {
		return e, err
	}
	if len(l.Objects) != 0 {
		return e, errors.New("PMC version listing contains unexpected objects")
	}
	versions := []int{}
	for _, entry := range l.Prefixes {
		stem, hasSlash := strings.CutSuffix(entry.Prefix, "/")
		got, v, valid := SplitPMCID("pmc:" + stem)
		if !hasSlash || !valid || got != paper || v == 0 || slices.Contains(versions, v) {
			return e, errors.New("PMC returned an invalid or duplicate version")
		}
		versions = append(versions, v)
	}
	if len(versions) == 0 {
		return e, errors.New("PMC has no distributed version of this article")
	}
	slices.Sort(versions)
	if version == 0 {
		version = versions[len(versions)-1]
	}
	if !slices.Contains(versions, version) {
		return e, errors.New("PMC does not distribute that version")
	}
	stem := paper + "." + strconv.Itoa(version)
	e.ID = "pmc:" + stem
	e.MetadataURL = "https://" + PMCHost + "/metadata/" + stem + ".json"
	b, err := p.record(ctx, e.MetadataURL)
	if err != nil {
		return e, err
	}
	m, err := parsePMCMetadata(b)
	if err != nil {
		return e, errors.New("PMC returned invalid version metadata")
	}
	if m.PMCID != paper || m.Version != version {
		return e, errors.New("PMC metadata names another article or version")
	}
	e.MetadataSHA256 = digest(b)
	e.Title = cleanList([]string{m.Title}, 500)
	e.Dates = []string{fmt.Sprintf("version %d; versions present: %v", version, versions)}
	e.Publishers = cleanList([]string{m.Citation}, 500)
	if m.DOI != "" {
		e.Publishers = append(e.Publishers, "doi:"+textField(m.DOI, 200))
	}
	if m.PMID != nil && *m.PMID > 0 {
		e.Publishers = append(e.Publishers, fmt.Sprintf("pmid:%d", *m.PMID))
	}
	e.LandingPage = "https://pmc.ncbi.nlm.nih.gov/articles/" + stem + "/"
	e.Rights = []string{"PMC license_code for this version: " + textField(m.License, 100), "Source: NIH NLM NCBI PubMed Central (PMC) Article Datasets. NLM does not endorse Nemalo. This observation may not reflect current NLM data; redistribution remains subject to the article terms."}
	e.Restrictions["is_pmc_openaccess"] = pmcFlag(m.OpenAccess)
	e.Restrictions["is_manuscript"] = pmcFlag(m.Manuscript)
	e.Restrictions["is_historical_ocr"] = pmcFlag(m.HistoricalOCR)
	e.Restrictions["is_retracted"] = pmcFlag(m.Retracted)
	e.CheckedAt = time.Now().UTC()
	e.Complete = len(e.Title) > 0
	if m.PDFURL == "" {
		e.Limitations = append(e.Limitations, "PMC declares no PDF for this version.")
		return e, nil
	}
	// Accept only the exact canonical S3 key and a single MD5 query parameter.
	u, err := url.Parse(m.PDFURL)
	if err != nil {
		return e, errors.New("PMC PDF declaration is not a URL")
	}
	q, err := url.ParseQuery(u.RawQuery)
	key := stem + "/" + PMCPDFName(paper, version)
	if err != nil || u.Scheme != "s3" || u.Host != "pmc-oa-opendata" || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Path != "/"+key || len(q) != 1 || len(q["md5"]) != 1 || !validDigest(q.Get("md5"), 16) {
		return e, errors.New("PMC PDF declaration does not match the exact version object and MD5")
	}
	// List only the exact PDF key, avoiding an article's potentially large media list.
	objects, err := p.list(ctx, key, "")
	if err != nil {
		return e, err
	}
	if len(objects.Prefixes) != 0 || len(objects.Objects) != 1 || objects.Objects[0].Key != key || objects.Objects[0].Size <= 0 {
		return e, errors.New("PMC PDF listing does not name one positive-size exact object")
	}
	n := objects.Objects[0].Size
	f := OfferedFile{Name: PMCPDFName(paper, version), Format: "PDF", CandidateKind: "pdf_candidate", Bytes: &n, MD5: strings.ToLower(q.Get("md5")), Access: "discovery_only", PrivateState: "not_declared", RestrictionState: "not_declared", RightsStatus: "provider_version_declaration", DRMStatus: "not_assessed", Findings: []string{}}
	if m.OpenAccess != nil && *m.OpenAccess && pmcCC(m.License) {
		f.Access = PMCPDFAccess
		f.DownloadURL = pmcPDFURL(paper, version)
	} else {
		f.Findings = append(f.Findings, "acquisition requires a known Creative Commons license and explicit open-access subset declaration")
	}
	e.Files = append(e.Files, f)
	return e, nil
}

func (p *PMC) Download(ctx context.Context, file OfferedFile) (*http.Response, error) {
	stem, ok := strings.CutSuffix(file.Name, ".pdf")
	paper, v, valid := SplitPMCID("pmc:" + stem)
	if !ok || !valid || v == 0 || file.Access != PMCPDFAccess || file.DownloadURL != pmcPDFURL(paper, v) || !validDigest(file.MD5, 16) {
		return nil, errors.New("download does not match the selected PMC version PDF and MD5")
	}
	return p.requestPacer.get(ctx, p.client(), pmcPace, http.MethodGet, file.DownloadURL)
}
