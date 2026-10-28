# 0105: local EPUB covers in the terminal

Status: recorded before implementation, 2026-10-27. Fifth increment of the TUI
design tracker [issue 65](https://github.com/blisspixel/nemalo/issues/65):
[issue 72](https://github.com/blisspixel/nemalo/issues/72). It builds on
decisions [0101](0101-task-sections-and-action-registry.md) to
[0104](0104-terminal-feedback.md), and reads EPUBs only through the shared
container validation of [decision 0008](0008-filesystem-and-display-boundaries.md) and the
content services. It adds no module or dependency: Go's standard image decoders
and the Kitty helpers in `github.com/charmbracelet/x/ansi`, already required,
are enough.

## Context

Holding details are text only, although many EPUBs declare a cover image. Issue
65 decided that covers are shown where a terminal graphics protocol works, with
a title card elsewhere, from the local EPUB only and never fetched. An image is
untrusted bytes from an untrusted container. It needs its own threat and budget
review before any decoder runs on it.

## Threats

- **Decompression bombs.** A small PNG, GIF or JPEG can declare dimensions whose
  decoded pixels need gigabytes of memory.
- **Parser bugs and slow paths** in image decoders, including animated GIFs with
  many frames.
- **Confusion between declaration and bytes**: a member declared as a JPEG that
  is something else, or a cover chosen by guessing from a filename.
- **Escape sequences**: anything sent to the terminal from book data could
  inject control sequences.
- **Network and disk**: remote cover URLs, Open Library cover images, and graphics
  transmission modes that make the terminal or Nemalo read or write a file.
- **Terminal state**: images left behind after the panel closes, replies from the
  terminal arriving as keystrokes, and multiplexers that pass sequences on
  wrongly.
- **Responsiveness**: decoding on every selection change in a long list.

## Decision

1. **Declared cover only.** The cover comes from the first package that declares
   one: its manifest item with the EPUB 3 property `cover-image`, or else the
   item named by the EPUB 2 `<meta name="cover" content="ID">`. It must be a local member that the
   package validation already accounts for. A remote declaration, a missing
   item, or no declaration means no cover. Nemalo never picks an image by name,
   by spine position or by size.
2. **Shared validation.** `assessment.ReadEPUBCover` runs the same complete
   bounded container validation as every content read and returns only that
   member, at most 8 MiB. `content.Cover` opens the asset as content reads do,
   rechecks its identity, and hashes the whole file after the read, so a file
   changed during the read is refused. `app.Service.Cover` exposes it. It is a
   presentation read for the TUI; no CLI command or MCP tool is added.
3. **Media and sniffing.** The declared media type must be `image/jpeg`,
   `image/png` or `image/gif`, and Go's format sniffing of the bytes must name
   the same format. Other types (SVG, WebP and so on) are not decoded.
4. **Dimensions before decoding.** `image.DecodeConfig` reads only the header.
   Each side must be from 1 to 4096 pixels, and the area at most 8,388,608
   pixels (32 MiB as RGBA), before full decoding starts. GIF decodes only its
   first frame. Only `image/jpeg`, `image/png` and `image/gif` from the standard
   library are registered in the decoding package.
5. **Thumbnail in memory.** The decoded image is box-filtered to at most 280 by
   400 pixels and encoded as PNG in memory. At most 32 thumbnails are kept for
   the session; nothing is written to disk and nothing persists after exit.
6. **Protocol.** Kitty graphics with Unicode placeholders: the thumbnail is sent
   once with direct transmission (`t=d`, never a file, temporary file or shared
   memory), quiet mode `q=2` so the terminal sends no replies, and a virtual
   placement. The panel then draws ordinary placeholder cells (U+10EEEE with row
   and column diacritics) whose 256-colour foreground is the image number, so the
   cell renderer, scrolling and clipping handle the image like text. Image
   numbers are 16 to 255; this colour is protocol data, not a meaning colour.
   WezTerm does not draw placeholders. There the same transmission carries a
   placement at the cover box's screen cell (`C=1`, cursor saved and restored
   around it) over blank cells (amended 2026-10-28, below).
7. **Detection by environment only.** Covers are drawn in Kitty
   (`TERM=xterm-kitty` or `KITTY_WINDOW_ID`) and Ghostty (`TERM=xterm-ghostty` or
   `TERM_PROGRAM=ghostty`). Nemalo sends no query, so no reply can arrive as
   input. Covers are off under tmux or screen (`TMUX`, `STY`, or a `screen` or
   `tmux` `TERM`), in plain mode, without colour (`NO_COLOR` or fewer than 256
   colours), and when `"covers": "off"` is in config.json or `NEMALO_COVERS=off`
   is set. WezTerm (`TERM_PROGRAM=WezTerm`) uses the direct placement in the
   amendment below. iTerm2, foot and Windows Terminal show the title card:
   iTerm2 inline images and Sixel attach pixels to the cells they are drawn in,
   and the cell renderer's later writes erase them.
8. **Title card.** A rounded box of the same size (14 columns by 10 rows), with
   the wrapped title and the format, stands in while the cover loads, when there
   is no cover, on any refusal or failure, and wherever images are not drawn. A
   `Cover` line below it states the outcome: the declared format and size, none
   declared, the refusal reason, or that the terminal is not supported. Below 60
   columns, and in plain mode, neither the cover column nor the `Cover` line is
   shown, so the layout never depends on whether an image loaded. Without a
   protocol nothing is read.
9. **When images are sent and cleared.** The cover is read only when a holding's
   details open, never when moving through the list. Its image is deleted from
   the terminal (`a=d,d=I`) when the details close, when the step changes, on
   resize (it is sent again if the details are still open), and at exit.
10. **No network and no writes.** Cover URLs from providers, including Open
    Library, are never requested. Reading a cover takes no lock and writes
    nothing.

## Amendment 2026-10-28: WezTerm

A probe in a real WezTerm window (nightly 20261026) showed that a Kitty
graphics placement survives text written into its cells, that iTerm2 inline
images are erased cell by cell, that re-placing an image with `a=p` removes it,
and that WezTerm's Kitty graphics are on by default (also in the 20240203
release). In Nemalo itself the first WezTerm placement was missing or cut to a
strip. The renderer writes queued raw sequences before the next frame, and that
frame's erase-character writes (`ECH`) blanking the box cleared the image.

So in WezTerm:

- The cover box is blank cells while a thumbnail exists, so the layout never
  depends on whether the image is placed.
- The placement waits 100 ms after the state that needs it, so the frame that
  blanks its cells has reached the terminal; later frames leave unchanged cells
  alone. A wait is keyed to the asset and cell, and a stale wait is ignored.
- The screen cell comes from the same frame layout the panel uses. The image is
  deleted and placed again (`a=T`, never `a=p`) when that cell changes, and it is
  deleted while the Actions palette, help, the message log or a review covers the
  panel, or while the details are scrolled, because it floats above the text.
- Everything else (limits, transmission, deletion, detection order, off
  switches) is unchanged.

## Verification

Tests cover both cover declarations, remote and missing declarations, media
mismatches, sniffing mismatches, the member budget, a changed source, PNG, GIF
and JPEG headers whose dimensions exceed the limits (refused before full
decoding), malformed images, thumbnail bounds, the placeholder cells and
transmission sequences, environment detection, the config switch, the title
card at the same size, 500 holdings without any cover read, and deletion on
close, step change, resize and exit. A program test checks that the Bubble Tea
renderer emits the placeholder cells unchanged. On 2026-10-27 the released
`0.1.0-alpha.22` executable drew a declared cover in real Ghostty 1.3.1 and
Kitty 0.49.2 windows and removed it when the details closed
([validation](../VALIDATION.md#covers-in-real-kitty-and-ghostty-windows)).
For WezTerm, a test checks the placement's exact cell against the frame, its
wait, and its removal for an overlay and a scroll. A replay of the TUI's real
output found 20 writes into placed cells without the wait and none with it
([validation](../VALIDATION.md#covers-in-real-kitty-and-ghostty-windows)).
