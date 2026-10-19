package assessment

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/conformance"
)

func encryptionXML(entries ...string) string {
	return `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">` + strings.Join(entries, "") + `</encryption>`
}

func encrypted(algorithm, uri string) string {
	return `<enc:EncryptedData><enc:EncryptionMethod Algorithm="` + algorithm + `"/><enc:CipherData><enc:CipherReference URI="` + uri + `"/></enc:CipherData></enc:EncryptedData>`
}

const body = `<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Readable text.</p></body></html>`

func inspectBook(t *testing.T, mutate func(map[string]string)) Checks {
	t.Helper()
	data := book(t, body, mutate)
	c, err := InspectContext(context.Background(), bytes.NewReader(data), int64(len(data)), "epub")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Font obfuscation is a fact, not a finding; anything else encrypted, a
// wrapped key, a DRM license, or an unreadable declaration restricts access.
func TestFontObfuscationIsDistinguishedFromDRM(t *testing.T) {
	withFonts := func(decl string) func(map[string]string) {
		return func(f map[string]string) {
			f["fonts/a.otf"], f["fonts/b%20c.woff2"] = "obfuscated", "obfuscated"
			f["META-INF/encryption.xml"] = decl
		}
	}
	obfuscated := inspectBook(t, withFonts(encryptionXML(
		encrypted(idpfObfuscation, "fonts/a.otf"),
		encrypted(adobeObfuscation, "fonts/b%2520c.woff2"),
	)))
	if len(obfuscated.Warnings) != 0 || len(obfuscated.AccessRestrictions) != 0 || !slices.Equal(obfuscated.EPUB.ObfuscatedFonts, []string{"fonts/a.otf", "fonts/b%20c.woff2"}) {
		t.Fatal(obfuscated.Warnings, obfuscated.AccessRestrictions, obfuscated.EPUB.ObfuscatedFonts)
	}
	for name, mutate := range map[string]func(map[string]string){
		"aes content":     withFonts(encryptionXML(encrypted("http://www.w3.org/2001/04/xmlenc#aes128-cbc", "chapter.xhtml"))),
		"obfuscated text": withFonts(encryptionXML(encrypted(idpfObfuscation, "chapter.xhtml"))),
		"missing font":    withFonts(encryptionXML(encrypted(idpfObfuscation, "fonts/none.otf"))),
		"escaping target": withFonts(encryptionXML(encrypted(idpfObfuscation, "https://example.com/a.otf"))),
		"wrapped key":     withFonts(encryptionXML(`<enc:EncryptedKey/>`, encrypted(idpfObfuscation, "fonts/a.otf"))),
		"no reference":    withFonts(encryptionXML(`<enc:EncryptedData><enc:EncryptionMethod Algorithm="` + idpfObfuscation + `"/></enc:EncryptedData>`)),
		"not xml":         withFonts("declaration"),
		"wrong root":      withFonts(`<encryption>` + encrypted(idpfObfuscation, "fonts/a.otf") + `</encryption>`),
		"dtd":             withFonts(`<!DOCTYPE x [<!ENTITY e "x">]>` + encryptionXML()),
		"adobe rights":    func(f map[string]string) { f["META-INF/rights.xml"] = "<rights/>" },
		"lcp license":     func(f map[string]string) { f["META-INF/license.lcpl"] = "{}" },
		"two roots":       withFonts(encryptionXML() + encryptionXML()),
	} {
		c := inspectBook(t, mutate)
		if len(c.Warnings) == 0 || len(c.AccessRestrictions) == 0 {
			t.Fatal(name, "was not restricted", c.Warnings)
		}
	}
	// An empty declaration encrypts nothing.
	if c := inspectBook(t, withFonts(encryptionXML())); len(c.Warnings) != 0 || len(c.AccessRestrictions) != 0 {
		t.Fatal("an empty declaration was restricted", c.Warnings)
	}
}

// An obfuscated font is never handed out as if it were the font.
func TestObfuscatedFontIsNotServedAsAResource(t *testing.T) {
	data := book(t, body, func(f map[string]string) {
		f["fonts/a.otf"], f["image2.png"] = "obfuscated", "plain"
		f["META-INF/encryption.xml"] = encryptionXML(encrypted(idpfObfuscation, "fonts/a.otf"))
	})
	if _, _, err := VisitEPUBResource(context.Background(), bytes.NewReader(data), int64(len(data)), nil, "fonts/a.otf", 1<<20); !errors.Is(err, errObfuscatedFont) {
		t.Fatal(err)
	}
	if _, got, err := VisitEPUBResource(context.Background(), bytes.NewReader(data), int64(len(data)), nil, "image2.png", 1<<20); err != nil || string(got) != "plain" {
		t.Fatal(got, err)
	}
}

// A requested EPUBCheck that cannot run is a finding, never a pass, and the
// option leaves other formats alone.
func TestRequestedEPUBCheckThatCannotRunIsAFinding(t *testing.T) {
	saved := conformance.LookPath
	conformance.LookPath = func() (string, error) { return "", errors.New("not installed") }
	t.Cleanup(func() { conformance.LookPath = saved })
	p := write(t, "clean.epub", book(t, body, nil))
	plain, err := Check(context.Background(), p, Options{}, nil)
	if err != nil || plain.Status != "limited_checks_passed" || plain.Conformance != nil {
		t.Fatal(plain.Status, err)
	}
	r, _ := Check(context.Background(), p, Options{EPUBCheck: true}, nil)
	if r.Status != "needs_review" || r.Conformance == nil || r.Conformance.Status != "unavailable" || !slices.ContainsFunc(r.Findings, func(f string) bool { return strings.Contains(f, "EPUBCheck") }) {
		t.Fatal(r.Status, r.Conformance, r.Findings)
	}
	track := write(t, "track.mp3", mp3Frames(10))
	if r, err := Check(context.Background(), track, Options{EPUBCheck: true}, nil); err != nil || r.Conformance != nil || r.Status != "limited_checks_passed" {
		t.Fatal(r.Status, err)
	}
}
