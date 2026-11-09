package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
)

var modes = []string{"search", "inspect", "check", "scan", "snapshot", "holdings", "audit", "evaluate", "state", "read", "collections", "revisions"}

func modeShortcut(index int) string {
	if index < 10 {
		return strconv.Itoa((index + 1) % 10)
	}
	return string(rune('a' + index - 10))
}

type draft struct {
	input, second, content, status  string
	intake                          string
	submittedInput, submittedSecond string
	offset, total                   int
	entries                         []entry
	selected                        int
	details, evidence               bool
}

func (m *model) switchMode(name string) tea.Cmd {
	if name == m.mode {
		return nil
	}
	if m.filter != nil {
		m.entries, m.selected, m.filter = m.filter.all, m.filter.before, nil
	}
	m.loadMore = 0
	m.drafts[m.mode] = draft{input: m.input.Value(), second: m.secondary.Value(), intake: m.intake.Value(), content: m.report, status: m.status, submittedInput: m.submittedInput, submittedSecond: m.submittedSecond, offset: m.offset, total: m.total, entries: m.entries, selected: m.selected, details: m.details, evidence: m.evidence}
	m.mode = name
	d := m.drafts[name]
	m.input.SetValue(d.input)
	m.secondary.SetValue(d.second)
	m.intake.SetValue(d.intake)
	m.intake.Blur()
	m.intakeFocused = false
	m.secondary.Blur()
	m.secondaryFocused = false
	m.offset, m.total = d.offset, d.total
	m.submittedInput, m.submittedSecond = d.submittedInput, d.submittedSecond
	m.setReport(d.content)
	m.entries, m.details, m.evidence = d.entries, d.details, d.evidence
	m.resultsFocused = false
	m.selectEntry(d.selected)
	if name == "home" && len(m.entries) == 0 && m.report == "" && m.input.Value() == "" {
		m.homeWelcome()
	}
	m.status = d.status
	if m.status == "" {
		m.status = m.arrival()
	}
	m.viewport.GotoTop()
	m.resize()
	return m.input.Focus()
}

// Sentences the screens add when a result summary has already taken the status
// line. They name the keys in this terminal, and they do not run anything.
const (
	guideDownloadReview  = "Enter downloads this one file into the folder above. It is not in the library until Manage, F8, reviews the import."
	guideResumeReview    = "Enter continues this download. It is not in the library until Manage, F8, reviews the import."
	guideRecordingReview = "Enter downloads every track into the folder above. They are not in the library until Manage, F8, reviews the import."
	guideImportReview    = "After that, open Holdings and press r to read it here. In Read, p is pages and s is one word at a time, paused at 250 words per minute."
	guideAfterDownload   = "In this screen: open Manage, press F8, then Enter. That copies this folder into the library. It is not in the library yet."
	guideAfterImport     = "In this screen: open Holdings, press F6, then r. That reads it here. o opens a verified copy. In Read, p is pages and s is one word at a time, paused at 250 words per minute. Closing saves nothing."
)

// appendGuide adds one next-step sentence to the report on screen.
func (m *model) appendGuide(text string) {
	if text == "" || strings.Contains(m.report, text) {
		return
	}
	m.setReport(strings.TrimRight(m.report, "\n") + "\n\n" + text + "\n")
}

// nextCue is the next key for the selected row. It uses the blank line under
// that row, so it stays visible when the status line is the result itself
// (decision 0110).
func (m *model) nextCue() string {
	if m.busy || m.reviewing() || len(m.entries) == 0 {
		return ""
	}
	lead := "F6, then "
	if m.resultsFocused {
		lead = ""
	}
	switch m.mode {
	case "home":
		return "Enter opens this."
	case "search":
		id := selectedID(m)
		if strings.HasPrefix(id, "archive:") || strings.HasPrefix(id, "gutenberg:") || discovery.ValidOpenLibraryWork(id) || discovery.ValidOpenAlexWork(id) {
			return lead + "e prepares Evaluate. It does not download."
		}
		return "Enter shows this item. Nothing is downloaded."
	case "evaluate":
		if m.lastEvaluation != nil && m.lastEvaluation.OpenAccess != nil {
			if locationIdentifier(selectedID(m)) {
				return lead + "e prepares that identifier. It does not download."
			}
			return "This list does not download."
		}
		if m.lastEvaluation != nil && m.lastEvaluation.Access == discovery.OpenLibraryEditionList {
			return lead + "e selects this edition. It does not download."
		}
		if m.lastEvaluation != nil && m.lastEvaluation.Source == "gutenberg" && m.lastEvaluation.Access == "local_catalog" && len(m.lastEvaluation.Files) == 0 {
			return lead + "d requests the record. It does not download an ebook."
		}
		return lead + "d reviews one download into a new folder."
	case "holdings":
		e := m.entries[m.selected]
		if e.audio || e.mp4 {
			if e.managed {
				return lead + "o opens a verified copy in a player."
			}
			return lead + "r lists this track. It is not played here."
		}
		if e.managed {
			return lead + "r reads here. o opens a verified copy."
		}
		return lead + "r reads this here."
	case "read":
		switch m.reader.representation {
		case "mpeg-audio/1":
			return lead + "Enter retrieves frame ranges. Audio is not played here."
		case "mp4-audio/1":
			return lead + "u lists chapters. Audio is not played here."
		default:
			return lead + "p reads pages. s reads one word at a time, paused."
		}
	default:
		return ""
	}
}

// arrival is the first status of a step: the next action. It stays within two
// lines at 40 columns, where a longer status is replaced by a truncation
// notice (decision 0110).
func (m *model) arrival() string {
	switch m.mode {
	case "home":
		return "Enter opens a start. Then search, evaluate and download."
	case "search":
		if len(m.entries) > 0 {
			return "F6, then e prepares Evaluate. It does not download."
		}
		return "Enter searches. Nothing is downloaded."
	case "evaluate":
		if len(m.entries) > 0 {
			if m.lastEvaluation != nil && m.lastEvaluation.OpenAccess != nil {
				return "F6, then e prepares a pmc: or arxiv: identifier."
			}
			if m.lastEvaluation != nil && m.lastEvaluation.Access == discovery.OpenLibraryEditionList {
				return "F6, then e selects this edition. It does not download."
			}
			return "F6, then d reviews a download into a new folder."
		}
		return "Enter evaluates. d reviews a download. Nothing is fetched yet."
	case "holdings":
		if len(m.entries) > 0 {
			return "F6, then r reads here and o opens a verified copy."
		}
		return "Enter lists this library. Import from Manage with F8."
	case "read":
		if len(m.entries) > 0 {
			return "F6, then p reads pages and s reads one word at a time."
		}
		if m.reader.asset != "" {
			return "Enter lists parts. Then F6, p for pages, s for words."
		}
		return "In Holdings, r opens Read. p is pages. s is one word at a time."
	case "state":
		return "F7 reviews a new library. F8 reviews an import. Each needs Enter."
	default:
		return "Ready."
	}
}

func (m *model) seedLibraryForms(catalog, root string) {
	for _, mode := range []string{"holdings", "audit"} {
		d := m.drafts[mode]
		if mode == m.mode {
			continue
		}
		if d.input == "" {
			d.input = catalog
			if mode == "audit" {
				d.second = root
			}
			m.drafts[mode] = d
		}
	}
}

func (m *model) turnPage(forward bool) tea.Cmd {
	workPage := m.mode == "evaluate" && discovery.ValidOpenLibraryWork(m.submittedInput)
	if m.busy || (m.mode != "search" && m.mode != "holdings" && !workPage) {
		return nil
	}
	if m.input.Value() != m.submittedInput || m.secondary.Value() != m.submittedSecond {
		m.status = "Inputs changed. Press Enter to start a new query."
		return nil
	}
	size, ceiling := 10, 10000
	if m.mode == "holdings" {
		size, ceiling = 50, 100000
	}
	next := max(0, m.offset-size)
	if forward {
		next = m.offset + size
	}
	if next == m.offset || next > ceiling || next >= m.total {
		return nil
	}
	m.offset = next
	return m.start(false)
}

func (m *model) start(reset bool) tea.Cmd {
	if m.mode == "read" {
		return m.startRead(true, 0, "", false)
	}
	if m.mode == "jobs" {
		return m.observeJobs()
	}
	if m.mode == "home" {
		return m.readOverview()
	}
	if m.busy || m.input.Value() == "" {
		return nil
	}
	// Audit of an initialized library directory needs no root: it is a check.
	libraryCheck := m.mode == "audit" && m.secondary.Value() == "" && isDir(m.input.Value())
	if (m.mode == "snapshot" || m.mode == "audit") && m.secondary.Value() == "" && !libraryCheck {
		m.status = "Enter the required second field with Ctrl+N."
		return nil
	}
	if (m.mode == "collections" || m.mode == "revisions") && m.secondary.Value() == "" {
		m.status = "Enter the required second field with Ctrl+N."
		m.resize()
		return nil
	}
	if reset {
		m.offset, m.total = 0, 0
	}
	m.submittedInput, m.submittedSecond = m.input.Value(), m.secondary.Value()
	ctx, cancel := context.WithCancel(m.ctx)
	if m.mode == "collections" || m.mode == "revisions" {
		cancel()
		ctx, cancel = context.WithTimeout(m.ctx, 15*time.Second)
	}
	m.cancel = cancel
	m.id++
	id, mode, value, source, order := m.id, m.mode, m.input.Value(), m.source, m.searchOrder()
	if mode == "state" && m.initializeAction {
		mode = "initialize"
	} else if mode == "state" && m.importAction {
		mode = "import"
	} else if mode == "state" && m.rescanAction {
		mode = "rescan"
	} else if mode == "state" && m.cleanupPlan {
		mode = "cleanupplan"
	} else if mode == "state" && m.restorePlan {
		mode = "restoreplan"
	} else if mode == "state" && m.restoreAction {
		mode = "restore"
	} else if mode == "state" && m.cleanupDigest != "" {
		mode = "cleanup"
	} else if mode == "state" && m.purgePlan {
		mode = "purgeplan"
	} else if mode == "state" && m.purgeDigest != "" {
		mode = "purge"
	}
	digest, purgeDigest := m.cleanupDigest, m.purgeDigest
	m.purgePlan, m.purgeDigest = false, ""
	m.initializeAction, m.importAction, m.rescanAction, m.cleanupPlan, m.cleanupDigest = false, false, false, false, ""
	m.restorePlan, m.restoreAction = false, false
	second, assess, offset, format := m.secondary.Value(), m.assess, m.offset, m.format
	limits := inventory.Defaults()
	if m.largeBudget {
		limits.TotalBytes = 5 << 30
	}
	m.busy = true
	m.status = "Working. Escape cancels; Ctrl+C quits."
	m.setReport("")
	m.populate(nil)
	m.resize()
	return func() tea.Msg {
		switch mode {
		case "initialize":
			data, err := m.service.InitializeLibrary(ctx, value)
			return result{id, data, err}
		case "import":
			// A plain folder (not an intake packet) is ingested as a whole.
			if info, statErr := os.Lstat(second); statErr == nil && info.IsDir() {
				if _, receiptErr := os.Lstat(filepath.Join(second, "receipt.json")); receiptErr != nil {
					data, err := m.service.Ingest(ctx, library.IngestRequest{Library: value, Folder: second, Apply: true})
					return result{id, data, err}
				}
			}
			data, err := m.service.Import(ctx, library.ImportRequest{Library: value, Source: second, Apply: true})
			return result{id, data, err}
		case "rescan":
			data, err := m.service.Rescan(ctx, library.RescanRequest{Library: value, Apply: true})
			return result{id, data, err}
		case "restoreplan", "restore":
			data, err := m.service.Restore(ctx, library.RestoreRequest{Library: value, CleanupID: strings.TrimSpace(second), Apply: mode == "restore"})
			return result{id, data, err}
		case "purgeplan", "purge":
			request := library.PurgeRequest{Library: value, CleanupID: strings.TrimSpace(second)}
			if mode == "purge" {
				request.Apply = purgeDigest
			}
			data, err := m.service.Purge(ctx, request)
			return result{id, data, err}
		case "cleanupplan":
			data, err := m.service.Cleanup(ctx, library.CleanupRequest{Library: value, Folder: second})
			return result{id, data, err}
		case "cleanup":
			data, err := m.service.Cleanup(ctx, library.CleanupRequest{Library: value, Folder: second, Apply: digest})
			return result{id, data, err}
		case "state":
			data, err := m.service.LibraryStatus(ctx, value)
			return result{id, data, err}
		case "collections":
			data, err := m.service.CollectionStatus(ctx, second, value)
			return result{id, data, err}
		case "revisions":
			data, err := m.service.CollectionDiff(ctx, value, second)
			return result{id, data, err}
		case "evaluate":
			if discovery.ValidOpenLibraryWork(value) {
				data, err := m.service.EvaluateOpenLibraryEditions(ctx, value, 10, offset)
				return result{id, data, err}
			}
			data, err := m.service.Evaluate(ctx, value)
			return result{id, data, err}
		case "snapshot":
			data, err := m.service.Snapshot(ctx, value, second, limits, assess)
			return result{id, data, err}
		case "holdings":
			// A library directory lists managed holdings; a file is a snapshot.
			if info, statErr := os.Lstat(value); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				data, err := m.service.ManagedHoldings(ctx, value, second, format, "all", 50, offset)
				return result{id, data, err}
			}
			data, err := m.service.HoldingsFormat(value, second, format, 50, offset)
			return result{id, data, err}
		case "audit":
			if second == "" && isDir(value) {
				data, err := m.service.CheckLibrary(ctx, value)
				return result{id, data, err}
			}
			data, err := m.service.Audit(ctx, value, second, limits)
			return result{id, data, err}
		case "search":
			if source == "gutenberg" {
				path := strings.TrimSpace(second)
				if path == "" {
					path = m.catalog
				}
				data, err := m.service.SearchGutenberg(ctx, path, value, order, 10, offset)
				return result{id, data, err}
			}
			data, err := m.service.SearchSourceOrdered(ctx, source, value, order, 10, offset)
			return result{id, data, err}
		case "check", "scan":
			data, err := m.service.Check(ctx, value, assessment.Options{Scan: mode == "scan"})
			return result{id, data, err}
		default:
			data, err := m.service.Inspect(ctx, value, limits)
			return result{id, data, err}
		}
	}
}

// searchOrder is the selected result order. Archive can use downloads or
// identifier. Gutenberg can use identifier. Anything else stays relevance.
func (m *model) searchOrder() string {
	switch m.source {
	case "archive":
		if m.order == discovery.OrderDownloads || m.order == discovery.OrderIdentifier {
			return m.order
		}
	case "gutenberg":
		if m.order == discovery.OrderIdentifier {
			return m.order
		}
	}
	return discovery.OrderRelevance
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
