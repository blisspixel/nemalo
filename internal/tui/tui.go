// Package tui presents shared operations in a keyboard-driven terminal interface.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/assessment"
	"github.com/blisspixel/nemalo/internal/content"
	"github.com/blisspixel/nemalo/internal/discovery"
	"github.com/blisspixel/nemalo/internal/inventory"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/present"
	"github.com/blisspixel/nemalo/internal/reader"
	"github.com/blisspixel/nemalo/internal/textsafe"
)

type result struct {
	id   int
	data any
	err  error
}

type model struct {
	ctx              context.Context
	service          app.Service
	input            textinput.Model
	secondary        textinput.Model
	secondaryFocused bool
	assess           bool
	height           int
	width            int
	color            bool
	dark             bool
	largeBudget      bool
	drafts           map[string]draft
	offset           int
	total            int
	submittedInput   string
	submittedSecond  string
	viewport         viewport.Model
	report           string
	preview          viewport.Model
	entries          []entry
	selected         int
	resultsFocused   bool
	details          bool
	evidence         bool
	confirmKind      string
	confirmRoot      string
	confirmSource    string
	confirmReport    string
	confirmStatus    string
	initializeAction bool
	importAction     bool
	rescanAction     bool
	// cleanupPlan runs a cleanup preview; cleanupDigest, once reviewed, applies it.
	cleanupPlan   bool
	cleanupDigest string
	// restorePlan previews a restore of the cleanup ID in the source field;
	// restoreAction applies it after review.
	restorePlan, restoreAction bool
	mode                       string
	source                     string
	order                      string
	catalog                    string
	format                     string
	status                     string
	busy                       bool
	id                         int
	cancel                     context.CancelFunc
	reader                     readerState
	// pager, when open, fills the screen; readerStop is where it was closed.
	pager      *pager
	readerStop *reader.Position
	pagerSeq   int
	// initial is a command run at start, such as the reader's first fetch.
	initial         tea.Cmd
	downloadReview  *acquisition.Request
	recordingReview *acquisition.RecordingRequest
	// lastEvaluation lets a detected download offer the item's other files.
	lastEvaluation *discovery.Evaluation
}

func newModel(ctx context.Context, service app.Service) *model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 1000
	input.Placeholder = "Search books (sent to Open Library on Enter)"
	input.SetWidth(70)
	input.SetVirtualCursor(true)
	input.SetStyles(textinput.Styles{Cursor: textinput.CursorStyle{Blink: true}})
	secondary := textinput.New()
	secondary.Prompt = ""
	secondary.CharLimit = 1000
	secondary.SetWidth(70)
	secondary.SetVirtualCursor(true)
	secondary.SetStyles(textinput.Styles{Cursor: textinput.CursorStyle{Blink: true}})
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(16))
	view.SoftWrap = true
	m := &model{ctx: ctx, service: service, input: input, secondary: secondary, width: 80, height: 25, dark: true, viewport: view, mode: "search", source: "openlibrary", format: "epub", drafts: map[string]draft{}, status: "Ready. Network activity only on explicit search/evaluation."}
	if g, ok := service.Providers["gutenberg"].(*discovery.Gutenberg); ok {
		m.catalog = g.Catalog
	}
	m.preview = viewport.New()
	m.preview.SoftWrap = true
	m.resize()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.input.Focus(), tea.RequestBackgroundColor, m.initial)
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case pagerChunk:
		return m, m.pagerChunk(msg)
	case pagerTick:
		return m, m.pagerTick(msg)
	case tea.KeyPressMsg:
		if m.pager != nil {
			if msg.String() == "ctrl+c" {
				m.closePager()
				return m, tea.Quit
			}
			return m, m.pagerKey(msg.String())
		}
	}
	switch msg := message.(type) {
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
	case tea.ColorProfileMsg:
		profile := msg.Profile.String()
		m.color = (profile == "ANSI" || profile == "ANSI256" || profile == "TrueColor") && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = msg.Height
		m.input.SetWidth(max(1, msg.Width-4))
		m.secondary.SetWidth(max(1, msg.Width-4))
		m.viewport.SetWidth(max(1, msg.Width-2))
		m.resize()
		if m.pager != nil {
			m.pagerLayout()
			return m, tea.Batch(m.pagerMore(), tea.ClearScreen)
		}
	case result:
		if msg.id != m.id {
			return m, nil
		}
		m.busy = false
		if m.cancel != nil {
			m.cancel()
			m.cancel = nil
		}
		m.status = "Complete."
		// A cleanup preview with something to move becomes a review; Enter
		// applies exactly that plan digest.
		if value, ok := msg.data.(library.CleanupResult); ok && msg.err == nil && !value.Applied && value.Move > 0 {
			m.confirmKind, m.confirmRoot, m.confirmSource, m.confirmReport, m.confirmStatus = "cleanup", value.Library, value.Folder, m.report, m.status
			m.cleanupDigest = value.Digest
			m.status = "Review cleanup. Enter moves the listed files to the holding area; Escape returns without moving."
			m.setReport(present.Cleanup(value) + "\nEnter moves exactly these entries. A changed folder refuses the plan. Restore with library restore.\n")
			m.viewport.GotoTop()
			m.resize()
			return m, nil
		}
		// A restore preview with files to return becomes a review.
		if value, ok := msg.data.(library.RestoreResult); ok && msg.err == nil && !value.Applied && value.Restore > 0 {
			m.confirmKind, m.confirmRoot, m.confirmSource, m.confirmReport, m.confirmStatus = "restore", value.Library, value.CleanupID, m.report, m.status
			m.status = "Review restore. Enter returns the listed files; Escape returns without writing."
			m.setReport(present.Restore(value) + "\nEnter restores exactly these files. Nothing in the folder is replaced.\n")
			m.viewport.GotoTop()
			m.resize()
			return m, nil
		}
		// After moving files, the source field holds the cleanup ID, so F9
		// previews undoing it.
		if value, ok := msg.data.(library.CleanupResult); ok && msg.err == nil && value.Applied && value.Moved > 0 {
			m.secondary.SetValue(value.CleanupID)
			m.submittedSecond = value.CleanupID
		}
		// A recording manifest is shown beside the holdings list, which stays.
		if value, ok := msg.data.(library.RecordingsResult); ok {
			m.status = "Recording manifest. Track order is declared by the source; no listening is recorded."
			if msg.err != nil {
				m.status = fmt.Sprintf("Recording manifest failed: %q", msg.err.Error())
			} else {
				m.setReport(present.Recordings(value))
				m.evidence = true
			}
			m.resize()
			return m, nil
		}
		if value, ok := msg.data.(library.Bookmark); ok && msg.err == nil {
			m.bookmarkStatus(fmt.Sprintf("Saved unit %d, offset %d. Closing does not save a bookmark.", value.Unit, value.Offset))
			return m, nil
		}
		if value, ok := msg.data.(app.BookmarkResume); ok {
			m.populate(nil)
			m.setReport(present.BookmarkResume(value.Status, value.Detail, value.Bookmark, value.Content))
			m.status = value.Detail
			m.viewport.GotoTop()
			if value.Status == "restored" && value.Content != nil && value.Content.Locator != nil {
				m.remember(*value.Content)
				loc := value.Content.Locator
				switch value.Content.Representation {
				case "epub-text/2", "pdf-text/1":
					m.resize()
					return m, m.openPager(m.input.Value(), m.secondary.Value(), value.Bookmark.AssetID, value.Content.Representation, reader.Position{Unit: loc.Unit, Offset: loc.Start}, false, reader.DefaultRate)
				case "epub-nav/1":
					if len(value.Content.Navigation) == 1 && len(value.Content.Navigation[0].Entries) == 1 && value.Bookmark.Cursor != "" {
						entry := value.Content.Navigation[0].Entries[0]
						cmd := m.openChapter(m.input.Value(), m.secondary.Value(), value.Bookmark.AssetID, value.Content.Navigation[0].Evidence, entry.Order, reader.Position{Unit: loc.Unit, Offset: loc.Start}, false)
						if m.pager != nil {
							m.pager.chapterCursor = value.Bookmark.Cursor
							m.pager.representationID = loc.RepresentationID
						}
						m.resize()
						return m, cmd
					}
				}
			}
			m.resize()
			return m, nil
		}
		// Opening a copy leaves the holdings list and report in place.
		if value, ok := msg.data.(library.ExportResult); ok {
			m.status = fmt.Sprintf("Opened a verified read-only copy: %q", value.Path)
			if msg.err != nil {
				m.status = fmt.Sprintf("Open failed: %q", msg.err.Error())
			}
			m.resize()
			return m, nil
		}
		switch value := msg.data.(type) {
		case acquisition.Result:
			if msg.err == nil {
				m.status = acquisitionStatus(value.Antivirus, m.service.ScanGuidance(value.Antivirus, true))
				for _, mode := range []string{"check", "scan"} {
					d := m.drafts[mode]
					d.input = value.ContentPath
					m.drafts[mode] = d
				}
			}
		case acquisition.RecordingResult:
			if msg.err == nil {
				m.status = fmt.Sprintf("Downloaded %d track(s) to untrusted intake. Ingest the recording directory into a library to keep their order.", len(value.Tracks))
			}
		case discovery.Evaluation:
			if msg.err == nil {
				m.lastEvaluation = &value
			}
		case discovery.Page:
			if msg.err == nil {
				m.offset, m.total = value.Offset, value.Total
			}
		case library.Page:
			if msg.err == nil {
				m.offset, m.total = value.Offset, value.Total
			}
		case library.ManagedPage:
			if msg.err == nil {
				m.offset, m.total = value.Offset, value.Total
			}
		case library.Snapshot:
			if msg.err == nil {
				m.seedLibraryForms(value.Output, value.Catalog.RootHint)
			}
		case content.Result:
			if msg.err == nil {
				m.remember(value)
				m.reader.next = value.Continuation
				m.reader.boundCatalog, m.reader.boundRoot = m.reader.last.Catalog, m.reader.last.Root
				switch value.Status {
				case "units_available":
					m.status = "Source units available. Select deliberately; no progress is recorded."
					if value.Representation == "epub-nav/1" {
						m.status = "Declared navigation. Enter reads one entry and stops at its declared end. Nothing is recorded."
					}
				case "partial_range":
					m.status = "Bounded range delivered; continuation available."
				default:
					m.status = "Selected range delivered; no reading progress is recorded."
				}
			}
		}
		if msg.err != nil {
			m.bookmarkStatus(fmt.Sprintf("Incomplete: %q", msg.err.Error()))
		}
		m.populate(msg.data)
		if msg.err != nil {
			m.populate(nil)
			m.evidence = true
		}
		if value, ok := msg.data.(acquisition.Result); ok && strings.HasPrefix(value.Status, "detected_") {
			m.offerOtherFiles(value)
		} else if ok && msg.err != nil && value.Output != "" && !value.Complete && m.lastEvaluation != nil && m.lastEvaluation.ID == m.submittedInput {
			// The packet is kept. Resume is offered only when a transfer record
			// and partial bytes exist; an early failure needs a new folder.
			m.populate(*m.lastEvaluation)
			m.evidence = true
			if value.TransferRecorded && value.Transfer.Bytes > 0 {
				m.status = fmt.Sprintf("Transfer stopped: %q. The packet was kept; select the same file and press d to review resuming it.", msg.err.Error())
			} else {
				m.status = fmt.Sprintf("Transfer stopped: %q. The packet was kept and has no transfer record. Start a new output.", msg.err.Error())
			}
		}
		data, err := textsafe.MarshalJSON(msg.data, true)
		switch value := msg.data.(type) {
		case acquisition.Result:
			data = []byte(present.Acquisition(value))
		case acquisition.RecordingResult:
			data = []byte(present.Recording(value))
		case library.State:
			data = []byte(present.LibraryState(value))
		case discovery.Evaluation:
			data = []byte(present.Evaluation(value))
		case library.Snapshot:
			data = []byte(present.Snapshot(value))
		case library.Page:
			data = []byte(present.Holdings(value))
		case library.ManagedPage:
			data = []byte(present.ManagedHoldings(value))
		case library.ExportResult:
			data = []byte(present.Export(value))
		case library.IngestResult:
			data = []byte(present.Ingest(value))
		case library.CleanupResult:
			data = []byte(present.Cleanup(value))
		case library.LibraryCheck:
			data = []byte(present.LibraryCheck(value))
		case library.RestoreResult:
			data = []byte(present.Restore(value))
		case library.Audit:
			data = []byte(present.Audit(value))
		case library.ImportResult:
			data = []byte(present.Import(value))
		case library.ManagedAudit:
			data = []byte(present.ManagedAudit(value))
		case library.RescanResult:
			data = []byte(present.Rescan(value))
		case assessment.Report:
			data = []byte(present.Health(value))
		case discovery.Page:
			if msg.err != nil {
				data = nil
			} else {
				data = []byte(present.Search(value))
			}
		case inventory.Report:
			data = []byte(present.Inventory(value))
		case content.Result:
			data = []byte(present.Content(value))
		}
		if err != nil {
			m.status = fmt.Sprintf("Output error: %q", err.Error())
			m.setReport("")
		} else {
			if msg.err != nil {
				data = append([]byte(fmt.Sprintf("Operation error: %q\n\n", msg.err.Error())), data...)
			}
			m.setReport(string(data))
			m.viewport.GotoTop()
		}
		m.resize()
		if !m.resultsFocused && !m.input.Focused() && !m.secondary.Focused() {
			return m, m.focusForm()
		}
	case tea.KeyPressMsg:
		if m.reviewingDownload() {
			return m, m.confirmDownload(msg)
		}
		if m.confirmRoot != "" {
			return m, m.confirmInitialization(msg)
		}
		if msg.String() == "f7" && m.mode == "state" && !m.busy {
			m.requestInitialization()
			return m, nil
		}
		if msg.String() == "f8" && m.mode == "state" && !m.busy {
			m.requestImport()
			return m, nil
		}
		if msg.String() == "ctrl+r" && m.mode == "state" && !m.busy {
			m.requestRescan()
			return m, nil
		}
		if msg.String() == "f9" && m.mode == "state" && !m.busy {
			return m, m.requestCleanup()
		}
		if handled, cmd := m.browserKey(msg.String()); handled {
			return m, cmd
		}
		if strings.HasPrefix(msg.String(), "alt+") {
			for i, name := range modes {
				if msg.String() == fmt.Sprintf("alt+%d", (i+1)%10) && !m.busy {
					return m, m.switchMode(name)
				}
			}
		}
		switch msg.String() {
		case "f4":
			if m.mode == "holdings" && !m.busy {
				formats := []string{"epub", "pdf", "mp3", "all"}
				for i, format := range formats {
					if format == m.format {
						m.format = formats[(i+1)%len(formats)]
						break
					}
				}
				m.offset, m.total = 0, 0
				m.setReport("")
				m.populate(nil)
				m.status = "Format filter changed. Press Enter to load holdings."
				m.resize()
			}
			return m, nil
		case "f3":
			if !m.busy && (m.mode == "inspect" || m.mode == "snapshot" || m.mode == "audit") {
				m.largeBudget = !m.largeBudget
			}
			if !m.busy && m.mode == "search" && m.source == "archive" {
				orders := []string{discovery.OrderRelevance, discovery.OrderDownloads, discovery.OrderIdentifier}
				for i, order := range orders {
					if order == m.searchOrder() {
						m.order = orders[(i+1)%len(orders)]
						break
					}
				}
				m.offset, m.total = 0, 0
				m.setReport("")
				m.populate(nil)
				m.status = "Archive result order: " + m.order + ". Press Enter to search."
				m.resize()
			}
			if !m.busy && m.mode == "search" && m.source == "gutenberg" {
				orders := []string{discovery.OrderRelevance, discovery.OrderIdentifier}
				for i, order := range orders {
					if order == m.searchOrder() {
						m.order = orders[(i+1)%len(orders)]
						break
					}
				}
				m.offset, m.total = 0, 0
				m.setReport("")
				m.populate(nil)
				m.status = "Gutenberg result order: " + m.order + ". Press Enter to read the local catalog."
				m.resize()
			}
			return m, nil
		case "ctrl+right", "ctrl+left":
			return m, m.turnPage(msg.String() == "ctrl+right")
		case "f2":
			if m.mode == "search" && !m.busy {
				switch m.source {
				case "openlibrary":
					m.source = "archive"
				case "archive":
					m.source = "gutenberg"
				default:
					m.source = "openlibrary"
				}
				m.order = discovery.OrderRelevance
				m.offset, m.total = 0, 0
				m.setReport("")
				m.populate(nil)
				if m.source == "gutenberg" {
					m.status = "Ready. Search provider: gutenberg. Enter reads the local catalog. The website is not queried."
				} else {
					m.status = "Ready. Search provider: " + m.source + ". Enter searches that provider on the network."
					if m.secondaryFocused {
						m.secondaryFocused = false
						m.secondary.Blur()
					}
				}
				m.resize()
			}
			return m, nil
		case "ctrl+n":
			if !m.form() || m.busy {
				return m, nil
			}
			m.secondaryFocused = !m.secondaryFocused
			m.resultsFocused = false
			m.resize()
			if m.secondaryFocused {
				m.input.Blur()
				return m, m.secondary.Focus()
			}
			m.secondary.Blur()
			return m, m.input.Focus()
		case "ctrl+a":
			if m.mode == "snapshot" && !m.busy {
				m.assess = !m.assess
				return m, nil
			}
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "esc":
			if m.cancel != nil {
				m.cancel()
				m.cancel = nil
			}
			m.id++
			m.busy = false
			m.status = "Cancelled. Any started download remains in its intake folder."
			m.resize()
			if !m.resultsFocused {
				return m, m.focusForm()
			}
			return m, nil
		case "tab", "shift+tab":
			if m.busy {
				return m, nil
			}
			step := 1
			if msg.String() == "shift+tab" {
				step = -1
			}
			return m, m.switchMode(modes[(m.modeIndex()+step+len(modes))%len(modes)])
		case "enter":
			return m, m.start(true)
		case "pgup", "pgdown":
			var cmd tea.Cmd
			if !m.evidence && len(m.entries) > 0 && (m.details || m.width >= 100) {
				m.preview, cmd = m.preview.Update(msg)
			} else {
				m.viewport, cmd = m.viewport.Update(msg)
			}
			return m, cmd
		}
	}
	// The reader owns the screen. An unhandled message, including the
	// clear-screen command, must not reach the query field.
	if m.pager != nil {
		return m, nil
	}
	if m.busy {
		return m, nil
	}
	if m.resultsFocused {
		return m, nil
	}
	var cmd tea.Cmd
	if m.secondaryFocused {
		m.secondary, cmd = m.secondary.Update(message)
	} else {
		m.input, cmd = m.input.Update(message)
	}
	return m, cmd
}

func (m *model) form() bool {
	if m.mode == "search" {
		return m.source == "gutenberg"
	}
	return m.mode == "snapshot" || m.mode == "holdings" || m.mode == "audit" || m.mode == "read" || m.mode == "evaluate" || m.mode == "state"
}

func interactiveTerminal(in io.Reader, out io.Writer) bool {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	if !inOK || !outOK {
		return false
	}
	return term.IsTerminal(inFile.Fd()) && term.IsTerminal(outFile.Fd())
}

func Run(ctx context.Context, service app.Service, in io.Reader, out io.Writer) error {
	if !interactiveTerminal(in, out) {
		return errors.New("tui requires an interactive terminal")
	}
	m := newModel(ctx, service)
	defer func() {
		if m.cancel != nil {
			m.cancel()
		}
	}()
	_, err := tea.NewProgram(m, tea.WithContext(ctx), tea.WithInput(in), tea.WithOutput(out)).Run()
	return err
}
