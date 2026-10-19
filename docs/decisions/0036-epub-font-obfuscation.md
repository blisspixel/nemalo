# 0036: EPUB font obfuscation is not DRM

Status: accepted and implemented, 2026-10-19. Refines the EPUB checks of
[decision 0009](0009-managed-epub-import.md) and the access restrictions of
[decision 0011](0011-default-text-with-located-gaps.md), for milestone 3.

## Context

Any `META-INF/encryption.xml` used to be a finding and an access restriction,
"may include font obfuscation". EPUB Open Container Format font obfuscation
scrambles the first bytes of an embedded font so it is not trivially reused;
the text is unaffected and the book needs no key. A DRM-free book with
obfuscated fonts therefore stayed in review and could not be read through
content access.

## Decision

The declarations are parsed with a strict XML decoder that rejects a DTD,
bounded to 1 MiB, 200,000 tokens, and 32 levels, under the OCF `encryption`
root. Each `EncryptedData` entry whose algorithm is IDPF font obfuscation
(`http://www.idpf.org/2008/embedding`) or Adobe font obfuscation
(`http://ns.adobe.com/pdf/enc#RC`) and whose `CipherReference` resolves,
relative to the container root, to an existing `.otf`, `.ttf`, `.ttc`, `.woff`,
or `.woff2` member is recorded in `obfuscated_fonts`. That is a fact, not a
finding.

Everything else keeps the book in review with an access restriction:

- any other algorithm, an obfuscated non-font or missing member, a remote or
  query-bearing reference, or an entry without a reference;
- an `EncryptedKey`, which wraps a key for a reader or vendor;
- a declaration that is not XML, lacks the OCF root, has two roots, or has a
  DTD (`encryption declarations unreadable`);
- an Adobe `META-INF/rights.xml` (`drm_rights_declaration`) or a Readium
  `META-INF/license.lcpl` (`drm_license`), with or without encryption.

An empty declaration encrypts nothing and is not a finding. A font-obfuscated
book can be checked and read like any other. `content resource` refuses an
obfuscated font member rather than return its scrambled bytes as a font.
Nothing is deobfuscated.

## Validation

Assessment tests record two obfuscated fonts (IDPF, and Adobe with an escaped
name) as a fact, and restrict AES content, obfuscated text, a missing font, a
remote reference, a wrapped key, an entry without a reference, a non-XML
declaration, a wrong root, two roots, a DTD, an Adobe rights file, and an LCP
license. A resource read refuses the obfuscated font and returns a plain image.
A content test reads a font-obfuscated book, and the structured-access test
restricts AES-encrypted reading-order content.
