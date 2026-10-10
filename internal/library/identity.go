package library

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/blisspixel/nemalo/internal/assessment"
)

const (
	originEPUBIdentifier  = "epub_package_identifier"
	confidenceStated      = "scheme_stated"
	confidencePreserved   = "preserved"
	maxIdentifiersOmitted = 2_000_000
)

// Identity is one namespaced identifier copied from a package, not a record
// Nemalo assigned and not a reason to merge files.
type Identity struct {
	Role          string `json:"role"`
	ID            string `json:"id"`
	Raw           string `json:"raw"`
	Scheme        string `json:"scheme,omitempty"`
	Origin        string `json:"origin"`
	Confidence    string `json:"confidence"`
	PackageUnique bool   `json:"package_unique,omitempty"`
}

func identitiesFrom(report assessment.Report) (list []Identity, omitted int, recorded bool) {
	if report.DetectedFormat != "epub" || report.Checks.EPUB == nil {
		return nil, 0, false
	}
	omitted = report.Checks.EPUB.IdentifiersOmitted
	seen := map[string]int{}
	for _, item := range report.Checks.EPUB.Identifiers {
		id, ok := classifyPackageIdentifier(item)
		if !ok {
			omitted++
			continue
		}
		if previous, exists := seen[id.ID]; exists {
			if id.PackageUnique {
				list[previous].PackageUnique = true
			}
			continue
		}
		if len(list) == 32 {
			omitted++
			continue
		}
		seen[id.ID] = len(list)
		list = append(list, id)
	}
	sortIdentities(list)
	if omitted > maxIdentifiersOmitted {
		omitted = maxIdentifiersOmitted
	}
	return list, omitted, true
}

func sortIdentities(list []Identity) {
	for i := 1; i < len(list); i++ {
		item := list[i]
		j := i
		for j > 0 && identityLess(item, list[j-1]) {
			list[j] = list[j-1]
			j--
		}
		list[j] = item
	}
}

func identityLess(a, b Identity) bool {
	if a.Role != b.Role {
		return a.Role < b.Role
	}
	return a.ID < b.ID
}

func classifyPackageIdentifier(in assessment.EPUBIdentifier) (Identity, bool) {
	raw := strings.TrimSpace(in.Value)
	scheme := strings.TrimSpace(in.Scheme)
	if raw == "" || len(raw) > 1000 || strings.ContainsAny(raw, "\x00\r\n") || len(scheme) > 64 || strings.ContainsAny(scheme, "\x00\r\n") {
		return Identity{}, false
	}
	out := Identity{Raw: raw, Origin: originEPUBIdentifier, PackageUnique: in.PackageUnique}
	if scheme != "" {
		out.Scheme = scheme
	}
	if role, id, ok := statedRole(raw, scheme); ok {
		out.Role, out.ID, out.Confidence = role, id, confidenceStated
		return out, true
	}
	sum := sha256.Sum256([]byte(scheme + "\n" + raw))
	out.Role = "unassigned"
	out.ID = "stated:" + hex.EncodeToString(sum[:])
	out.Confidence = confidencePreserved
	return out, true
}

func statedRole(raw, scheme string) (role, id string, ok bool) {
	folded := strings.ToLower(scheme)
	lower := strings.ToLower(raw)
	isbnScheme := folded == "isbn" || folded == "isbn-10" || folded == "isbn-13"
	if isbnScheme || (scheme == "" && strings.HasPrefix(lower, "urn:isbn:")) {
		if canonical, good := canonicalISBN(raw); good {
			return "edition", canonical, true
		}
		return "", "", false
	}
	if looksDOI(raw, scheme) {
		if body, good := canonicalDOI(raw); good {
			return "publication_version", "doi:" + body, true
		}
		return "", "", false
	}
	if kind, key, good := openLibraryKey(raw, scheme); good {
		if kind == "work" {
			return "work", "openlibrary_work:" + key, true
		}
		return "edition", "openlibrary_edition:" + key, true
	}
	return "", "", false
}

func looksDOI(raw, scheme string) bool {
	folded := strings.ToLower(scheme)
	if folded != "" && folded != "doi" && folded != "url" {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(lower, "doi:") || strings.HasPrefix(lower, "https://doi.org/") || strings.HasPrefix(lower, "http://doi.org/") || strings.HasPrefix(lower, "https://dx.doi.org/") || strings.HasPrefix(lower, "http://dx.doi.org/") {
		return true
	}
	return folded == "doi" && strings.HasPrefix(lower, "10.")
}

func canonicalISBN(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(v), "urn:isbn:") {
		v = strings.TrimSpace(v[len("urn:isbn:"):])
	}
	var b strings.Builder
	for _, c := range v {
		switch {
		case c >= '0' && c <= '9':
			b.WriteRune(c)
		case c == 'X' || c == 'x':
			b.WriteByte('X')
		case c == '-' || c == ' ':
			continue
		default:
			return "", false
		}
	}
	digits := b.String()
	switch {
	case len(digits) == 13 && isbn13(digits):
		return "isbn:" + digits, true
	case len(digits) == 10 && isbn10(digits):
		return "isbn:" + digits, true
	default:
		return "", false
	}
}

func isbn13(d string) bool {
	sum := 0
	for i := 0; i < len(d); i++ {
		n := int(d[i] - '0')
		if d[i] < '0' || d[i] > '9' {
			return false
		}
		if i%2 == 0 {
			sum += n
		} else {
			sum += 3 * n
		}
	}
	return sum%10 == 0
}

func isbn10(d string) bool {
	sum := 0
	for i := 0; i < 9; i++ {
		if d[i] < '0' || d[i] > '9' {
			return false
		}
		sum += int(d[i]-'0') * (10 - i)
	}
	var n int
	switch d[9] {
	case 'X':
		n = 10
	default:
		if d[9] < '0' || d[9] > '9' {
			return false
		}
		n = int(d[9] - '0')
	}
	return (sum+n)%11 == 0
}

func canonicalDOI(raw string) (string, bool) {
	v := strings.TrimSpace(raw)
	lower := strings.ToLower(v)
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
		if strings.HasPrefix(lower, prefix) {
			v = v[len(prefix):]
			break
		}
	}
	if !strings.HasPrefix(strings.ToLower(v), "10.") {
		return "", false
	}
	body := "10." + v[3:]
	if !validDOIBody(body) {
		return "", false
	}
	return body, true
}

func validDOIBody(body string) bool {
	if len(body) < 8 || len(body) > 220 || !strings.HasPrefix(body, "10.") {
		return false
	}
	rest := body[3:]
	slash := strings.IndexByte(rest, '/')
	if slash < 2 || slash > 9 {
		return false
	}
	for i := 0; i < slash; i++ {
		if rest[i] < '0' || rest[i] > '9' {
			return false
		}
	}
	suffix := rest[slash+1:]
	if suffix == "" || len(suffix) > 200 {
		return false
	}
	for i := 0; i < len(suffix); i++ {
		c := suffix[i]
		if c <= ' ' || c >= 0x7f || strings.ContainsRune("\\\"'<>#?", rune(c)) {
			return false
		}
	}
	return true
}

func openLibraryKey(raw, scheme string) (kind, key string, ok bool) {
	folded := strings.ToLower(scheme)
	if folded != "" && folded != "url" && folded != "openlibrary" {
		return "", "", false
	}
	v := strings.TrimSpace(raw)
	lower := strings.ToLower(v)
	var prefix, mark, role string
	switch {
	case strings.HasPrefix(lower, "https://openlibrary.org/works/"):
		prefix, mark, role = "https://openlibrary.org/works/", "W", "work"
	case strings.HasPrefix(lower, "https://openlibrary.org/books/"):
		prefix, mark, role = "https://openlibrary.org/books/", "M", "edition"
	default:
		return "", "", false
	}
	rest := strings.TrimSuffix(v[len(prefix):], "/")
	if rest == "" || strings.ContainsAny(rest, "/?#") || len(rest) < 4 || len(rest) > 16 {
		return "", "", false
	}
	if !strings.EqualFold(rest[:2], "ol") || !strings.EqualFold(rest[len(rest)-1:], mark) {
		return "", "", false
	}
	digits := rest[2 : len(rest)-1]
	if digits == "" || digits[0] == '0' || len(digits) > 12 {
		return "", "", false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return "", "", false
		}
	}
	return role, "OL" + digits + strings.ToUpper(mark), true
}

func validIdentities(list []Identity, recorded bool, omitted int) bool {
	if omitted < 0 || omitted > maxIdentifiersOmitted {
		return false
	}
	if !recorded {
		return list == nil && omitted == 0
	}
	if len(list) > 32 {
		return false
	}
	seen := map[string]bool{}
	var previous Identity
	for i, item := range list {
		if seen[item.ID] || !validIdentity(item) {
			return false
		}
		if i > 0 && !identityLess(previous, item) {
			return false
		}
		seen[item.ID] = true
		previous = item
	}
	return true
}

func validIdentity(item Identity) bool {
	got, ok := classifyPackageIdentifier(assessment.EPUBIdentifier{Value: item.Raw, Scheme: item.Scheme, PackageUnique: item.PackageUnique})
	return ok && got == item
}

func sameIdentity(a, b Holding) bool {
	if a.IdentitiesRecorded != b.IdentitiesRecorded || a.IdentifiersOmitted != b.IdentifiersOmitted || len(a.Identities) != len(b.Identities) {
		return false
	}
	for i := range a.Identities {
		if a.Identities[i] != b.Identities[i] {
			return false
		}
	}
	return true
}
