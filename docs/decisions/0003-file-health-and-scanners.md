# File health evidence and optional antivirus

Date: 2026-10-08. Updated: 2026-10-09. Status: accepted and implemented.

`internal/assessment` owns bounded file and EPUB container assessment, reused by
the collection harness and application services. CLI/TUI call `internal/app`;
`internal/scanner` is the one installed-antivirus adapter. Inventory remains a
cheap filename inventory; it does not silently scan or validate files.

Check actual signatures, measured bytes, SHA-256, ZIP CRCs, package references,
reading order, and measured HTML body text. Compare supplied expected size/hash
when available. Do not invent a minimum size, page estimate, completeness score,
or universal novel length. An image book, short poem, translation, and scanned
edition have different legitimate characteristics. Semantic completeness needs
edition/source comparison. EPUBs do not have a universal fixed page count.
PDF header/end markers and MP3 signatures are explicitly limited checks, with
page trees, embedded PDF actions, media decoding, and duration still unchecked.

Add the Go project's BSD-3-Clause
[`golang.org/x/net/html` v0.60.0](https://pkg.go.dev/golang.org/x/net/html@v0.60.0).
The standard library lacks an HTML5 tokenizer. This maintained implementation
handles entities and token normalization without a second runtime, cgo, or new
transitive runtime modules. Pin checksums, run govulncheck, and review updates.
Input must be UTF-8; document bytes and individual tokens are bounded. Tokenizer
measurements and positive active-content indicators are not DOM validation,
sanitization, or proof that unflagged content is safe. Preserve original bytes.
Removal is straightforward if a future mature EPUB parser supplies the same facts.

Optional antivirus uses installed
[ClamAV](https://docs.clamav.net/manual/Usage/Scanning.html) or Windows
[Defender](https://learn.microsoft.com/en-us/defender-endpoint/command-line-arguments-microsoft-defender-antivirus).
These are specialized external capabilities, not first-party application runtimes.
Do not implement a malware engine in Go or automatically install/update software.
Use argument arrays, no remediation options, a two-minute deadline, 64 KiB process
output, and a private snapshot whose hash is checked again after scanning.
ClamAV requests alerts for encrypted content and exceeded scan limits. Unknown
output, errors, truncation, missing tools, and cancellation cannot mean clean.
Only recognized no-detection output produces `no_detections_reported`; internal
coverage and signature freshness are not independently established. Localized
Defender output that cannot be interpreted remains incomplete.

Scanning is explicit and recommended for a PDF, ebook, or other file the user
did not produce. The app still runs when no scanner is installed. A missing
scan stays `not_scanned` and cannot mean clean.

The default scanner is the one this adapter already runs. Windows uses the
built-in Microsoft Defender command `MpCmdRun.exe`. Linux and macOS use
installed ClamAV `clamscan`. ClamAV is the free local engine with an official
command-line scanner and signature updates through `freshclam`. It is not
claimed to be the strongest detector, and Nemalo does not install or update it.
macOS does not provide an equivalent command-line scanner. On Windows, ClamAV
is a second engine, not the default. `doctor` names the recommendation for the
current operating system.

A publisher SHA-256 confirms that the received bytes are the published bytes.
Pass it to `check --expected-sha256`, or keep it in an intake receipt. Nemalo
also records the SHA-256 it measured. A matching checksum does not prove those
bytes are free of malware. A file and its published checksum can both be replaced.

Installed scanner cloud/sample-submission settings still
apply and are disclosed before invocation; Nemalo changes none of them. Local
assessment itself makes no network requests. Temporary permissions and processes
are not security sandboxes. No reader, script, macro, or downloaded executable is
launched. Scanner evidence alone never authorizes publication or source cleanup.
