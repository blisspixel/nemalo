package assessment

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"
)

// Font obfuscation algorithms. They scramble the start of an embedded font so
// it is not trivially reused; they are not DRM, and apply only to fonts.
const (
	idpfObfuscation  = "http://www.idpf.org/2008/embedding"
	adobeObfuscation = "http://ns.adobe.com/pdf/enc#RC"
)

const containerNS = "urn:oasis:names:tc:opendocument:xmlns:container"

var fontExtensions = map[string]bool{".otf": true, ".ttf": true, ".ttc": true, ".woff": true, ".woff2": true}

// errObfuscatedFont refuses to hand out an obfuscated font as if its bytes
// were the font.
var errObfuscatedFont = errors.New("EPUB resource is an obfuscated font; its stored bytes are not the font")

// epubEncryption reads META-INF/encryption.xml and the DRM license files.
// Font obfuscation of font members is recorded as a fact. Anything else
// encrypted, an encrypted key, an unreadable declaration, or a DRM license
// is a finding and an access restriction, because a reader may not be able
// to open the content without a key Nemalo does not hold.
func epubEncryption(files map[string]*zip.File, c *Checks) {
	restrict := func(warning, restriction string) {
		c.Warnings = append(c.Warnings, warning)
		if !containsString(c.AccessRestrictions, restriction) {
			c.AccessRestrictions = append(c.AccessRestrictions, restriction)
		}
	}
	if files["META-INF/rights.xml"] != nil {
		restrict("Adobe DRM rights declaration present", "drm_rights_declaration")
	}
	if files["META-INF/license.lcpl"] != nil {
		restrict("Readium LCP license present", "drm_license")
	}
	if files["META-INF/encryption.xml"] == nil {
		return
	}
	data, err := member(files, "META-INF/encryption.xml", 1<<20)
	if err != nil {
		restrict("encryption declarations unreadable", "encryption_declarations")
		return
	}
	fonts, other, err := parseEncryption(data, files)
	if err != nil {
		restrict("encryption declarations unreadable", "encryption_declarations")
		return
	}
	if other {
		restrict("encrypted resources declared beyond font obfuscation; DRM or encryption", "encryption_declarations")
	}
	c.EPUB.ObfuscatedFonts = fonts
}

// parseEncryption returns the font members obfuscated with a known font
// obfuscation algorithm, and whether anything else is declared encrypted.
func parseEncryption(data []byte, files map[string]*zip.File) (fonts []string, other bool, err error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	var algorithm string
	var targets []string
	inData, depth, root := false, 0, false
	seen := map[string]bool{}
	finish := func() {
		for _, t := range targets {
			name, ok := cipherTarget(t)
			_, isFont := fontExtensions[strings.ToLower(path.Ext(name))]
			if ok && files[name] != nil && isFont && (algorithm == idpfObfuscation || algorithm == adobeObfuscation) {
				if !seen[name] {
					seen[name] = true
					fonts = append(fonts, name)
				}
				continue
			}
			other = true
		}
		if len(targets) == 0 {
			other = true
		}
	}
	for tokens := 0; ; tokens++ {
		if tokens > 200_000 {
			return nil, false, errors.New("encryption declarations outside limits")
		}
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > 32 {
				return nil, false, errors.New("encryption declarations nested too deeply")
			}
			if depth == 1 {
				if t.Name.Local != "encryption" || t.Name.Space != containerNS || root {
					return nil, false, errors.New("encryption declarations lack the OCF encryption root")
				}
				root = true
			}
			switch t.Name.Local {
			case "EncryptedKey":
				// A key wrapped for a reader or vendor is DRM, whatever it encrypts.
				other = true
			case "EncryptedData":
				inData, algorithm, targets = true, "", nil
			case "EncryptionMethod":
				if inData && algorithm == "" {
					algorithm = attr(t, "Algorithm")
				}
			case "CipherReference":
				if inData {
					targets = append(targets, attr(t, "URI"))
				}
			}
		case xml.EndElement:
			depth--
			if t.Name.Local == "EncryptedData" && inData {
				finish()
				inData = false
			}
		case xml.Directive:
			return nil, false, errors.New("encryption declarations contain a DTD")
		}
	}
	if !root {
		return nil, false, errors.New("encryption declarations lack the OCF encryption root")
	}
	sort.Strings(fonts)
	return fonts, other, nil
}

// cipherTarget resolves a CipherReference URI, relative to the container root.
func cipherTarget(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	name := strings.TrimPrefix(path.Clean("/"+u.Path), "/")
	return name, name != "" && name != "."
}

func attr(e xml.StartElement, local string) string {
	for _, a := range e.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
