package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/present"
)

type entry struct {
	title, summary, detail, sourceID string
	audio, pdf                       bool
	// mp4 marks an M4B or M4A, which lists its sample tables and chapters.
	mp4 bool
	// managed marks a holding of a library directory, which is read with the
	// same directory as catalog and root.
	managed bool
	// recording is the source ID of a declared recording this holding is a
	// track of, if any.
	recording string
	// nav marks one source-declared contents entry. navOrder is its index in
	// that navigation document, which is not a spine unit.
	nav                bool
	navEvidence        string
	navOrder           int
	navUnit, navOffset int
	navResolved        bool
	// page marks one declared page-list marker; pageRef reads from it and
	// pageUnit and pageOffset are its epub-text/2 position.
	page                 bool
	pageRef              string
	pageUnit, pageOffset int
	// identity is the full value y copies; cards show it shortened. right is
	// the list's right-aligned column, such as format and size.
	identity string
	right    string
	// step and bookmark are Home's destinations: a section's first step,
	// or an explicit saved bookmark to open in Read.
	step     string
	bookmark *library.Bookmark
	// epub marks a held EPUB, whose details can show its declared cover.
	epub bool
}

// Browsing is a presentation of shared service results, not another catalog.
// Source text is shown without Go quoting on one safe row, status tokens read
// as words, and empty values are left out, as in the command-line output.
func (m *model) populate(data any) {
	m.entries, m.selected, m.details, m.evidence = nil, 0, false, false
	m.filter, m.loadMore = nil, 0
	line, words, list := present.Line, present.Words, present.List
	switch p := data.(type) {
	case library.CollectionStatusResult, library.CollectionDiffResult, app.CollectionAcquireResult:
		m.entries = collectionEntries(p)
	case app.JobSnapshotResult:
		for _, j := range p.Jobs {
			c := card{title: firstOf(words(j.Kind), "Recorded job"), facts: nonEmpty(words(j.Relationship)), chips: append([]chip{jobChip(j.RecordedStatus)}, labelledChips("Owner", words(j.Owner), "Journal", words(j.Integrity))...), identity: j.ID}
			c.lists = append(c.lists, cardList{"Created", []string{line(j.Created)}}, cardList{"Updated", []string{line(j.Updated)}}, cardList{"Observed", []string{line(p.Finished)}})
			if j.Progress != nil {
				c.lists = append(c.lists, cardList{"Progress", []string{fmt.Sprintf("%s: %d of %d %s", line(j.Progress.Phase), j.Progress.Done, j.Progress.Total, line(j.Progress.Unit))}})
			}
			var notes []string
			if j.ErrorRecorded {
				notes = append(notes, "An error was recorded; raw error text is omitted.")
			}
			if j.HeldTracks > 0 {
				notes = append(notes, fmt.Sprintf("%d related held tracks do not establish a complete recording.", j.HeldTracks))
			}
			c.caveat = strings.Join(append(notes, "Recorded outcome only; unfinished owner liveness is not observed."), " ")
			m.entries = append(m.entries, entry{title: shortID(j.ID), summary: words(j.Kind), right: "recorded " + words(j.RecordedStatus), detail: m.render(c), identity: j.ID})
		}
	case discovery.Evaluation:
		if p.Access == discovery.OpenLibraryEditionList {
			for _, edition := range p.Editions {
				archive := "no Archive item named"
				if edition.ArchiveID != "" {
					archive = line(edition.ArchiveID)
				}
				m.entries = append(m.entries, entry{
					title:   line(edition.Title),
					summary: joinParts(list(edition.Languages), archive),
					detail: line(edition.Title) + "\n\n" + rows("ID", line(edition.ID), "Language", list(edition.Languages), "Publisher", list(edition.Publishers), "Date", list(edition.Dates), "ISBN", list(edition.ISBNs), "Archive item", archive, "Page", line(edition.LandingPage)) +
						findings(edition.Findings) + "\nPress e, then Enter, to read this edition. An ocaid then requests that Archive item. A different identifier in the edition's source records is not selected.\n",
					sourceID: edition.ID,
				})
			}
			if len(p.Editions) == 0 {
				m.entries = append(m.entries, entry{
					title:    "No editions on this page",
					summary:  "This page did not list an edition",
					detail:   "This page has no edition rows.\n\nAnother offset may. A work-level Archive identifier is not used.\n",
					sourceID: p.ID,
				})
			}
			m.selectEntry(0)
			return
		}
		for _, f := range p.Files {
			size := "size not declared"
			if f.Bytes != nil {
				size = byteSize(*f.Bytes)
			}
			c := card{title: line(f.Name), facts: nonEmpty(line(f.Format), size), chips: labelledChips("Access", words(f.Access), "Rights", words(f.RightsStatus), "DRM", words(f.DRMStatus))}
			if len(f.Findings) > 0 {
				c.lists = append(c.lists, cardList{"Findings", strings.Split(strings.TrimSpace(list(f.Findings)), ", ")})
			}
			if v := list(p.Rights); v != "" {
				c.lists = append(c.lists, cardList{"Item rights", []string{v}})
			}
			if v := list(p.LicenseURLs); v != "" {
				c.lists = append(c.lists, cardList{"Licenses", []string{v}})
			}
			c.caveat = "Declarations, not file rights or safety. Availability is resolved again before transfer."
			c.keys = "d reviews an explicit intake download"
			m.entries = append(m.entries, entry{title: line(f.Name), summary: words(f.Access), right: size, detail: m.render(c), sourceID: f.Name})
		}
		if len(p.Files) == 0 && p.Source == "gutenberg" && p.Access == "local_catalog" {
			m.entries = append(m.entries, entry{
				title:    "No files in the catalog row",
				summary:  "d requests the bibliographic record",
				detail:   "No files are listed in this catalog row.\n\nSet an intake folder (Ctrl+N), then press d. That requests one bibliographic record from the pinned mirror and lists EPUB offers. It does not download an ebook.\n\nSelect one EPUB and press d again to review the download.\n",
				sourceID: p.ID,
			})
		}
	case discovery.Page:
		for _, r := range p.Results {
			note := "Discovery metadata only; download availability not resolved."
			if r.Access == "local_catalog" {
				note = "Local catalog row; the Gutenberg website was not queried. No file or download URL is listed."
			}
			claim := present.CatalogClaim(r)
			c := card{title: line(r.Title), facts: nonEmpty(list(r.Authors), list(r.Languages)), chips: labelledChips("Declared", words(r.Access)), identity: r.ID, caveat: note}
			if claim != "" {
				c.chips = append(c.chips, chip{claim, muted})
			}
			if r.LandingPage != "" {
				c.lists = append(c.lists, cardList{"Source page", []string{line(r.LandingPage)}})
			}
			c.keys = "e prepares an evaluation; nothing is requested until Enter"
			m.entries = append(m.entries, entry{
				title:    line(r.Title),
				summary:  joinParts(list(r.Authors), list(r.Languages), claim),
				detail:   m.render(c),
				sourceID: r.ID, identity: r.ID,
			})
		}
	case library.Page:
		for _, a := range p.Assets {
			title, languages := shortID(a.ID), []string{}
			if len(a.Locations) > 0 {
				title = a.Locations[0].Path
			}
			if a.Health != nil && a.Health.Checks.EPUB != nil {
				e := a.Health.Checks.EPUB
				languages = e.Languages
				if len(e.Titles) > 0 {
					title = e.Titles[0]
				}
			}
			c := card{title: line(title), facts: nonEmpty(list(languages), fmt.Sprintf("%s (%d bytes)", byteSize(a.Bytes), a.Bytes), copies(len(a.Locations))), identity: a.ID, caveat: "Recorded when cataloged. Audit checks current bytes."}
			if a.Health != nil {
				c.chips = []chip{healthChip(a.Health.Status), antivirusChip(a.Health.Antivirus.Status)}
			} else {
				c.chips = []chip{healthChip(""), antivirusChip("")}
			}
			held := cardList{label: "Copies"}
			for _, l := range a.Locations {
				held.values = append(held.values, fmt.Sprintf("%s (%s)", line(l.Path), words(l.CandidateKind)))
			}
			c.lists = append(c.lists, held)
			c.keys = "r reads this asset"
			detail := m.render(c)
			m.entries = append(m.entries, entry{
				title:    line(title),
				summary:  joinParts(list(languages), copies(len(a.Locations))),
				right:    byteSize(a.Bytes),
				detail:   detail,
				sourceID: a.ID, identity: a.ID,
				audio: len(a.Locations) > 0 && strings.HasSuffix(strings.ToLower(a.Locations[0].Path), ".mp3"),
				pdf:   len(a.Locations) > 0 && strings.HasSuffix(strings.ToLower(a.Locations[0].Path), ".pdf"),
				mp4:   len(a.Locations) > 0 && (strings.HasSuffix(strings.ToLower(a.Locations[0].Path), ".m4b") || strings.HasSuffix(strings.ToLower(a.Locations[0].Path), ".m4a")),
			})
		}
	case library.ManagedPage:
		for _, h := range p.Holdings {
			title := h.Path
			if len(h.Titles) > 0 {
				title = h.Titles[0]
			}
			c := card{title: line(title), facts: nonEmpty(strings.ToUpper(words(h.Format)), list(h.Languages), fmt.Sprintf("%s (%d bytes)", byteSize(h.Bytes), h.Bytes)), chips: []chip{holdingChip(h.Status, h.Reason)}, identity: h.ID, caveat: "Recorded journal facts. Audit rechecks stored bytes."}
			c.lists = append(c.lists, cardList{"Stored at", []string{line(h.Path)}})
			ids := cardList{label: "Identifiers"}
			for _, id := range h.Identities {
				ids.values = append(ids.values, fmt.Sprintf("%s (%s)", line(id.ID), words(id.Role)))
			}
			if len(ids.values) > 0 {
				c.lists = append(c.lists, ids)
			}
			recording := ""
			sources := cardList{label: "Sources"}
			for _, src := range h.Sources {
				source := line(src.Path)
				if src.SourceID != "" {
					source = joinParts(line(src.SourceID), line(src.SourceFile), source)
				}
				sources.values = append(sources.values, source)
				if src.Recording != nil && src.SourceID != "" {
					sources.values = append(sources.values, fmt.Sprintf("  track %d of %d in %s (declared order)", src.Recording.Position, src.Recording.Tracks, line(src.Recording.Format)))
					recording = firstOf(recording, src.SourceID)
				}
			}
			if len(sources.values) > 0 {
				c.lists = append(c.lists, sources)
			}
			c.keys = "r reads  o opens a verified copy"
			if recording != "" {
				c.keys += "  t shows the whole recording"
			}
			detail := m.render(c)
			m.entries = append(m.entries, entry{title: line(title), summary: joinParts(list(h.Languages), words(h.Status)), right: strings.ToUpper(words(h.Format)) + "  " + byteSize(h.Bytes), detail: detail, sourceID: h.ID, identity: h.ID, audio: h.Format == "mp3", pdf: h.Format == "pdf", mp4: h.Format == "m4b" || h.Format == "m4a", managed: true, recording: recording, epub: h.Format == "epub"})
		}
	case content.Result:
		if p.Status == "units_available" && p.Audio != nil {
			a := p.Audio
			m.entries = append(m.entries, entry{title: "MP3 track: " + line(a.Format), summary: fmt.Sprintf("%d Hz   %d ch   %d frames   %d ms encoded", a.SampleRate, a.Channels, a.Frames, a.EncodedDurationMs), detail: fmt.Sprintf("MP3 track\n\nFormat: %s\nSample rate: %d Hz\nChannels: %d\nIndexed frames: %d\nEncoded duration: %d ms\nDeclared chapters: %d\nUnindexed regions: %d\n\nEnter retrieves a frame range from the start; n continues.\nAudio bytes are not shown or played here.\nNo listening progress is recorded.", line(a.Format), a.SampleRate, a.Channels, a.Frames, a.EncodedDurationMs, len(a.Chapters), len(a.Gaps))})
		} else if p.Status == "units_available" && p.MP4 != nil {
			a := p.MP4
			m.entries = append(m.entries, entry{title: "MP4 audio: " + words(a.Codec), summary: fmt.Sprintf("%d Hz   %d ch   %d samples   %s encoded", a.SampleRate, a.Channels, a.Samples, present.Clock(a.EncodedDurationMs)), detail: fmt.Sprintf("MP4 audio\n\nCodec: %s\nSample rate: %d Hz\nChannels: %d\nLocated samples: %d\nEncoded duration: %s\nDeclared chapters: %d\nFindings: %s\n\nEvery sample was located in the file; nothing was decoded.\nAudio is not played here: in Holdings, o opens a verified copy in a player.\nNo listening progress is recorded.", words(a.Codec), a.SampleRate, a.Channels, a.Samples, present.Clock(a.EncodedDurationMs), len(a.Chapters), firstOf(list(a.Findings), "none"))})
			for _, c := range a.Chapters {
				m.entries = append(m.entries, entry{title: "Chapter: " + firstOf(line(c.Title), line(c.ID)), summary: present.Clock(c.StartMs) + " to " + present.Clock(c.EndMs), detail: fmt.Sprintf("Declared chapter\n\nTitle: %s\nStart: %s\nEnd: %s\nSource: %s\n\nChapter times are the file's own declarations, not checked by decoding.\n", line(c.Title), present.Clock(c.StartMs), present.Clock(c.EndMs), words(a.ChapterSource))})
			}
		} else if p.Status == "units_available" && p.Representation == "pdf-text/1" {
			for _, u := range p.Units {
				label := pageLabel(u.PageLabel)
				m.entries = append(m.entries, entry{title: fmt.Sprintf("Page %d, label %s", u.Index, label), summary: fmt.Sprintf("%s   %d text bytes   %d gaps", words(u.Extraction), u.TextBytes, u.KnownGaps), detail: fmt.Sprintf("PDF page\n\nPhysical index: %d (zero-based)\nPrinted label: %s\nExtraction: %s\nText bytes: %d; located gaps: %d\n\nText is in content-stream order; columns are not reconstructed and there is no OCR.\nEnter retrieves a bounded range of this page.\nNo reading progress is recorded.", u.Index, label, words(u.Extraction), u.TextBytes, u.KnownGaps)})
			}
		} else if p.Representation == "epub-page-list/1" && (p.Status == "units_available" || p.Status == "no_page_list") {
			source := "Pagination source: not stated by the package."
			if p.Pagination != nil && p.Pagination.Status == "declared" {
				source = "Pagination source, as the package states it: " + list(append(append([]string{}, p.Pagination.Sources...), p.Pagination.PageBreakSources...))
			}
			if len(p.PageLists) == 0 {
				m.entries = append(m.entries, entry{title: "No declared page list", summary: "This EPUB declares no page labels", detail: "No page labels are made up from text length or screen pages.\n" + source + "\n"})
			}
			for _, l := range p.PageLists {
				if l.Problem != "" {
					m.entries = append(m.entries, entry{title: "Page list not used: " + words(l.Problem), summary: line(l.Path), detail: rows("Path", line(l.Path), "Problem", words(l.Problem)) + "\nThe source was recorded and not repaired.\n"})
					continue
				}
				for _, e := range l.Entries {
					where := words(e.Status)
					resolved := e.Status == "resolved" && e.Unit != nil && e.Offset != nil
					if resolved {
						where = fmt.Sprintf("unit %d, byte %d", *e.Unit, *e.Offset)
					}
					detail := fmt.Sprintf("Declared page label %s\n\n", line(e.Label)) + rows("Marker", fmt.Sprint(e.Order), "Status", words(e.Status), "Target", where, "Href", line(e.Href)) + "\n" + source + "\n" +
						"A page label is this EPUB's own marker. It is not a screen page, and it is not checked against a print edition.\nEnter reads from it; p pages from it. Neither records reading progress.\n"
					item := entry{title: "Page " + line(e.Label), summary: joinParts(where, fmt.Sprintf("marker %d", e.Order)), detail: detail, page: true, pageRef: e.Reference}
					if resolved {
						item.pageUnit, item.pageOffset = *e.Unit, *e.Offset
					}
					m.entries = append(m.entries, item)
				}
			}
		} else if p.Status == "units_available" && p.Representation == "epub-nav/1" {
			if len(p.Navigation) == 0 {
				m.entries = append(m.entries, entry{title: "No declared navigation", summary: "This EPUB has no NAV document and no NCX", detail: "Spine documents are still available with u.\nNo contents list was inferred from headings.\n"})
			}
			for _, doc := range p.Navigation {
				if doc.Problem != "" {
					m.entries = append(m.entries, entry{title: words(doc.Evidence) + ": " + words(doc.Problem), summary: "This navigation source was not used", detail: rows("Evidence", words(doc.Evidence), "Path", line(doc.Path), "Problem", words(doc.Problem)) + "\nThe source was recorded and not fetched or repaired.\n"})
					continue
				}
				for _, e := range doc.Entries {
					where := words(e.Status)
					if e.Unit != nil && e.Offset != nil {
						where = fmt.Sprintf("unit %d, byte %d", *e.Unit, *e.Offset)
					}
					parent := "none"
					if e.Parent != nil {
						parent = fmt.Sprintf("%d", *e.Parent)
					}
					indent := strings.Repeat("  ", max(0, e.Level-1))
					detail := fmt.Sprintf("Declared entry %d\n\n", e.Order) + rows("Label", line(e.Label), "Evidence", words(doc.Evidence), "Level", fmt.Sprint(e.Level), "Parent", parent, "Status", words(e.Status), "Target", where, "Href", line(e.Href)) + "\nEnter reads this entry and stops at its declared end.\np pages it. Neither records reading progress.\n"
					item := entry{title: indent + line(e.Label), summary: joinParts(words(doc.Evidence), fmt.Sprintf("L%d", e.Level), where), detail: detail, nav: true, navEvidence: doc.Evidence, navOrder: e.Order}
					if e.Status == "resolved" && e.Unit != nil && e.Offset != nil {
						item.navResolved, item.navUnit, item.navOffset = true, *e.Unit, *e.Offset
					}
					m.entries = append(m.entries, item)
				}
			}
		} else if p.Status == "units_available" {
			for _, u := range p.Units {
				m.entries = append(m.entries, entry{title: fmt.Sprintf("Unit %d: %s", u.Index, line(u.Member)), summary: joinParts(fmt.Sprintf("%d text bytes", u.TextBytes), fmt.Sprintf("%d gaps", u.KnownGaps), line(u.Language)), detail: fmt.Sprintf("Source unit %d\n\n", u.Index) + rows("Member", line(u.Member), "Language", line(u.Language), "Direction", line(u.Direction), "Linear", words(u.Linear)) + fmt.Sprintf("Parts: %d; known text gaps: %d\n\nEnter retrieves a bounded structured-text range.\nNo reading progress is recorded.", u.Parts, u.KnownGaps)})
			}
		}
	}
	m.selectEntry(0)
}

// rows writes "label: value" rows for label and value pairs, leaving out
// empty values. Values must already be safe for one row.
func rows(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			b.WriteString(pairs[i] + ": " + pairs[i+1] + "\n")
		}
	}
	return b.String()
}

// findings lists source findings, one per row, after a blank row.
func findings(values []string) string {
	var b strings.Builder
	for _, f := range values {
		if f = present.Words(f); f != "" {
			b.WriteString("Finding: " + f + "\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n" + b.String()
}

// joinParts joins the non-empty summary parts with spaces, without pipes.
func joinParts(parts ...string) string {
	shown := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			shown = append(shown, p)
		}
	}
	return strings.Join(shown, "   ")
}

func copies(n int) string {
	if n == 1 {
		return "1 copy"
	}
	return fmt.Sprintf("%d copies", n)
}

// pageLabel is a printed PDF page label, or "none".
func pageLabel(label string) string {
	if label = present.Line(label); label != "" {
		return label
	}
	return "none"
}

func byteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1<<20 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
}

func (m *model) selectEntry(index int) {
	if len(m.entries) == 0 {
		m.preview.SetContent("")
		return
	}
	m.selected = max(0, min(index, len(m.entries)-1))
	m.refreshPreview()
	m.preview.GotoTop()
}

func (m *model) refreshPreview() {
	if len(m.entries) > 0 {
		if e, ok := m.coverEntry(); ok {
			m.preview.SetContent(m.withCover(e, m.preview.Width()))
			return
		}
		// Pre-wrap at grapheme boundaries. The viewport's cell slicing can split
		// wide characters, causing its renderer to wrap an already sliced line.
		m.preview.SetContent(lipgloss.Wrap(m.entries[m.selected].detail, max(1, m.preview.Width()), ""))
	}
}

func (m *model) focusForm() tea.Cmd {
	m.resultsFocused = false
	m.resize()
	if m.intakeFocused {
		return m.intake.Focus()
	}
	if m.secondaryFocused {
		return m.secondary.Focus()
	}
	return m.input.Focus()
}

func (m *model) browserKey(key string) (bool, tea.Cmd) {
	if m.busy {
		return false, nil
	}
	switch key {
	case "f5":
		if m.report == "" {
			return true, nil
		}
		m.evidence = !m.evidence
		m.details = false
		m.resize()
		return true, nil
	case "esc":
		if m.details || m.evidence {
			m.details, m.evidence = false, false
			m.resize()
			return true, nil
		}
	case "f6":
		if m.resultsFocused {
			return true, m.focusForm()
		}
		m.resultsFocused = true
		m.input.Blur()
		m.secondary.Blur()
		m.intake.Blur()
		m.resize()
		return true, nil
	}
	if !m.resultsFocused {
		return false, nil
	}
	if key != "enter" {
		m.loadMore = 0
	}
	switch key {
	case "/":
		if len(m.entries) > 0 || m.filter != nil {
			return true, m.openFilter()
		}
	case "y":
		return true, m.copyIdentity()
	case "d":
		if m.mode == "evaluate" && len(m.entries) > 0 {
			return true, m.requestDownload()
		}
	case "a":
		if m.mode == "evaluate" && len(m.entries) > 0 && m.recordingFor(m.entries[m.selected].sourceID) != nil {
			m.requestRecording()
			return true, nil
		}
	case "esc":
		if m.filter != nil {
			m.clearFilter()
			return true, nil
		}
		return true, m.focusForm()
	case "up", "down", "home", "end":
		if !m.details && !m.evidence {
			// Past either end, paging continues (decision 0103).
			if key == "down" && m.selected == len(m.entries)-1 && m.morePages(true) {
				return true, m.continuePast(true)
			}
			if key == "up" && m.selected == 0 && m.morePages(false) {
				return true, m.continuePast(false)
			}
			index := m.selected
			switch key {
			case "up":
				index--
			case "down":
				index++
			case "home":
				index = 0
			case "end":
				index = len(m.entries) - 1
			}
			m.selectEntry(index)
			return true, nil
		}
	case "enter":
		if m.loadMore != 0 {
			forward := m.loadMore > 0
			m.loadMore = 0
			m.selectLast = !forward
			return true, m.turnPage(forward)
		}
		if m.mode == "home" && len(m.entries) > 0 && !m.details && !m.evidence && (m.entries[m.selected].step != "" || m.entries[m.selected].bookmark != nil) {
			return true, m.homeEnter()
		}
		if m.mode == "read" && len(m.entries) > 0 {
			return true, m.startRead(false, m.selected, "", false)
		}
		if len(m.entries) > 0 {
			m.details, m.evidence = !m.details, false
			m.resize()
		}
		return true, nil
	case "e":
		if len(m.entries) > 0 && m.mode == "search" {
			id := m.entries[m.selected].sourceID
			switch {
			case strings.HasPrefix(id, "archive:"):
				cmd := m.switchMode("evaluate")
				m.input.SetValue(id)
				m.status = "Selected item. Enter requests Archive metadata; no download."
				m.resize()
				return true, cmd
			case strings.HasPrefix(id, "gutenberg:"):
				path := strings.TrimSpace(m.submittedSecond)
				if path == "" {
					path = m.catalog
				}
				m.service.UseGutenbergCatalog(path)
				cmd := m.switchMode("evaluate")
				m.input.SetValue(id)
				m.status = "Selected item. Enter reads the local catalog. The website is not queried."
				m.resize()
				return true, cmd
			case discovery.ValidOpenLibraryWork(id):
				cmd := m.switchMode("evaluate")
				m.input.SetValue(id)
				m.status = "Selected work. Enter lists editions. No Archive item is chosen."
				m.resize()
				return true, cmd
			}
		}
		if len(m.entries) > 0 && m.mode == "evaluate" && discovery.ValidOpenLibraryEdition(m.entries[m.selected].sourceID) {
			m.input.SetValue(m.entries[m.selected].sourceID)
			m.offset, m.total = 0, 0
			m.status = "Selected edition. Enter reads its record. An ocaid then requests that Archive item."
			m.resize()
			return true, nil
		}
	case "r":
		if m.mode == "holdings" && len(m.entries) > 0 {
			// Capture the selection first: switching modes clears the entries.
			selected, catalog := m.entries[m.selected], m.submittedInput
			representation := "epub-structure/1"
			if selected.audio {
				representation = "mpeg-audio/1"
			}
			if selected.pdf {
				representation = "pdf-text/1"
			}
			if selected.mp4 {
				representation = "mp4-audio/1"
			}
			cmd := m.switchMode("read")
			m.reader = readerState{asset: selected.sourceID, representation: representation}
			m.input.SetValue(catalog)
			m.secondary.SetValue("")
			if selected.managed {
				// The explicit library directory is both catalog and root.
				m.secondary.SetValue(catalog)
			}
			m.setReport("")
			m.populate(nil)
			m.status = "Selected asset. Enter an explicit root, then Enter lists source units."
			m.resize()
			return true, cmd
		}
		if m.mode == "read" {
			return true, m.startRead(false, 0, "", true)
		}
	case "o":
		// Open hands the reader a verified read-only copy, never the stored file.
		if m.mode == "holdings" && len(m.entries) > 0 && m.entries[m.selected].managed {
			asset, libraryDir, service := m.entries[m.selected].sourceID, m.submittedInput, m.service
			ctx, cancel := context.WithCancel(m.ctx)
			m.cancel = cancel
			m.id++
			id := m.id
			m.busy = true
			m.status = "Copying and verifying a read-only copy for the default application."
			m.resize()
			return true, func() tea.Msg {
				data, err := service.Open(ctx, libraryDir, asset, "")
				return result{id: id, data: data, err: err}
			}
		}
	case "t":
		// The recording manifest reads and indexes the held tracks' bytes.
		if m.mode == "holdings" && len(m.entries) > 0 && m.entries[m.selected].recording != "" {
			source, libraryDir, service := m.entries[m.selected].recording, m.submittedInput, m.service
			ctx, cancel := context.WithCancel(m.ctx)
			m.cancel = cancel
			m.id++
			id := m.id
			m.busy = true
			m.status = "Verifying and indexing the recording's held tracks."
			m.resize()
			return true, func() tea.Msg {
				data, err := service.Recordings(ctx, libraryDir, source)
				return result{id: id, data: data, err: err}
			}
		}
	case "n":
		if m.mode == "read" && m.reader.next != "" {
			return true, m.startRead(false, 0, m.reader.next, false)
		}
	case "u":
		if m.mode == "read" {
			return true, m.startRead(true, 0, "", false)
		}
	case "c":
		if m.mode == "read" {
			return true, m.listNavigation()
		}
	case "g":
		if m.mode == "read" {
			return true, m.listPages()
		}
	case "m":
		if m.mode == "read" {
			return true, m.saveBookmark()
		}
	case "'":
		if m.mode == "read" {
			return true, m.restoreBookmark()
		}
	case "p", "s":
		if m.mode == "read" {
			return true, m.readSelected(key == "s")
		}
	}
	return false, nil
}

func clip(text string, width int) string {
	// Input is already quoted; Lip Gloss truncates by terminal cells, not bytes.
	if lipgloss.Width(text) > width && width > 3 {
		return lipgloss.NewStyle().MaxWidth(width-3).Render(text) + "..."
	}
	return lipgloss.NewStyle().MaxWidth(max(1, width)).Render(text)
}

// browseView is plain mode's results area: the list, with the preview beside
// it in wide terminals.
func (m *model) browseView() string {
	height, width := m.viewport.Height(), m.viewport.Width()
	if m.evidence || len(m.entries) == 0 {
		return m.viewport.View()
	}
	if m.details {
		return m.preview.View()
	}
	listWidth := width
	if width >= 98 {
		listWidth = width / 2
	}
	rows := m.resultRows(listWidth-2, height)
	if m.mode == "home" {
		rows = m.homeLines(listWidth-2, height)
	}
	list := lipgloss.NewStyle().Width(listWidth).Height(height).MaxHeight(height).Render(strings.Join(rows, "\n"))
	if listWidth == width {
		return list
	}
	divider := strings.TrimSuffix(strings.Repeat(" | \n", height), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, list, m.paint(divider, muted, false), m.preview.View())
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
