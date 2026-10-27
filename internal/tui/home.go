package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/library"
	"github.com/blisspixel/nemalo/internal/present"
)

// Home (decision 0102) is the default start: a bounded, read-only, offline
// overview of one library, explicit bookmarks, and where to go next. It
// reads through the same services as the CLI and writes nothing.

// homeOverview is one observation of a library for Home.
type homeOverview struct {
	library                  string
	state                    library.State
	stateErr                 error
	checked, review          int
	holdingsErr              error
	bookmarks                library.BookmarkState
	bookmarksErr             error
	jobs                     app.JobSnapshotResult
	jobsErr                  error
	jobsObserved, holdingsOK bool
}

var sectionUses = map[string]string{
	"Discover":    "Find books in Open Library, Internet Archive or a local Gutenberg catalog",
	"Library":     "Browse holdings, read, open a verified copy",
	"Collections": "Resolve exact selections and compare revisions",
	"Check files": "Inspect folders, check or scan files, snapshot and audit",
	"Manage":      "Library status, initialize, import, rescan, clean up, restore, purge",
	"Jobs":        "Recorded jobs for one library, observed without changing them",
}

// configure applies the configured defaults and opens Home. Fields stay
// editable; a configured library is read once, offline, for the overview,
// by the command it returns.
func (m *model) configure(o Options) tea.Cmd {
	m.library, m.doctor = o.Library, o.Doctor
	if m.library != "" {
		for step, fields := range map[string][2]string{
			"holdings": {m.library, ""}, "read": {m.library, m.library}, "state": {m.library, ""},
			"jobs": {m.library, ""}, "audit": {m.library, ""},
		} {
			d := m.drafts[step]
			d.input, d.second = fields[0], fields[1]
			m.drafts[step] = d
		}
		d := m.drafts["collections"]
		d.second = m.library
		m.drafts["collections"] = d
		d = m.drafts["home"]
		d.input = m.library
		m.drafts["home"] = d
	}
	m.switchMode("home")
	if m.library != "" {
		return m.start(true)
	}
	return nil
}

// homeWelcome is the first-run view: no library is configured or entered.
func (m *model) homeWelcome() {
	m.populate(nil)
	m.entries = []entry{
		{title: "Find a public-domain book", summary: "Discover: search Open Library, Internet Archive or a Gutenberg catalog", detail: "Search a provider, evaluate an item, then review an acquisition into a new intake folder. Search uses the network only when you press Enter; a local Gutenberg catalog does not.\n", step: "search"},
		{title: "Look at files I already have", summary: "Check files: inspect a folder or check one file, offline", detail: "Inspect lists a folder read-only; Check runs local health checks on one file. Nothing is moved, changed or uploaded.\n", step: "inspect"},
		{title: "Set up a library folder", summary: "Manage: enter an existing folder, then F7 reviews initialization", detail: "Initialization writes only control files, after a review and its own Enter. Import, rescan and cleanup are separate reviews.\n", step: "state"},
	}
	m.setReport(m.welcomeText())
	m.selectEntry(0)
}

func (m *model) welcomeText() string {
	text := "Welcome to Nemalo. Knowledge is power.\n\nNo library is configured. Enter an existing library folder above and press Enter for its overview, or choose a start below. Set \"library\" in config.json, NEMALO_LIBRARY or --library to open it here next time.\n"
	return text + "\n" + m.scannerLine() + "\n"
}

// scannerLine reports scanner availability from the doctor report; nothing
// is run.
func (m *model) scannerLine() string {
	if m.doctor == nil {
		return "Scanner    not checked"
	}
	for _, c := range m.doctor.Capabilities {
		if !c.Recommended {
			continue
		}
		if !c.Available {
			return "Scanner    " + present.Line(c.Name) + " not found; downloads stay unscanned"
		}
		line := "Scanner    " + present.Line(c.Name) + " available"
		if s := m.doctor.Signatures; s != nil && s.Status != "" {
			line += ", signatures " + present.Words(s.Status)
			if s.AgeDays != nil {
				line += fmt.Sprintf(" (%d days old)", *s.AgeDays)
			}
		}
		return line
	}
	return "Scanner    none recommended for this platform"
}

// readOverview is Home's Enter: local, bounded, read-only and cancellable.
func (m *model) readOverview() tea.Cmd {
	if m.busy || m.input.Value() == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	m.cancel = cancel
	m.id++
	id, dir, service := m.id, m.input.Value(), m.service
	m.submittedInput = dir
	m.busy = true
	m.status = "Reading the library overview, offline. Escape cancels."
	m.setReport("")
	m.populate(nil)
	m.resize()
	return func() tea.Msg {
		o := homeOverview{library: dir}
		o.state, o.stateErr = service.LibraryStatus(ctx, dir)
		if o.stateErr != nil || o.state.Status != "initialized" {
			return result{id, o, nil}
		}
		checked, err := service.ManagedHoldings(ctx, dir, "", "all", "checked", 1, 0)
		review, reviewErr := service.ManagedHoldings(ctx, dir, "", "all", "review", 1, 0)
		o.checked, o.review, o.holdingsErr = checked.Total, review.Total, firstErr(err, reviewErr)
		o.holdingsOK = o.holdingsErr == nil
		o.bookmarks, o.bookmarksErr = service.ListBookmarks(ctx, dir)
		if errors.Is(o.bookmarksErr, library.ErrBookmarksNotEnabled) {
			o.bookmarks, o.bookmarksErr = library.BookmarkState{}, nil
		}
		if service.JobsDirectory != "" {
			o.jobs, o.jobsErr = service.JobSnapshot(ctx, app.JobSnapshotRequest{Library: dir, JobsDirectory: service.JobsDirectory})
			o.jobsObserved = true
		}
		return result{id, o, nil}
	}
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// showOverview presents an observation as summary lines and go-to entries.
func (m *model) showOverview(o homeOverview) {
	var b strings.Builder
	state := present.Words(o.state.Status)
	switch {
	case o.stateErr != nil:
		state = "unavailable: " + present.Error(o.stateErr)
		m.outcome = outcomeAttention
	case o.state.Status == "not_initialized":
		state = "not initialized; Manage reviews initialization with F7"
		m.outcome = outcomeAttention
	case o.state.Status != "initialized":
		m.outcome = outcomeAttention
	default:
		m.outcome = outcomeOK
	}
	fmt.Fprintf(&b, "Library    %s\nStatus     %s\n", present.Exact(o.library), state)
	if o.state.Status == "initialized" && o.stateErr == nil {
		if o.holdingsOK {
			fmt.Fprintf(&b, "Holdings   %d checked, %d in review\n", o.checked, o.review)
		} else {
			fmt.Fprintf(&b, "Holdings   not read: %s\n", present.Error(o.holdingsErr))
		}
	}
	b.WriteString(m.scannerLine() + "\n")
	jobsSummary := ""
	if o.jobsObserved && o.jobsErr == nil {
		unfinished := 0
		for _, j := range o.jobs.Jobs {
			if j.RecordedStatus == "queued" || j.RecordedStatus == "running" {
				unfinished++
			}
		}
		jobsSummary = fmt.Sprintf("%d recorded, %d recorded as unfinished", len(o.jobs.Jobs), unfinished)
		fmt.Fprintf(&b, "Jobs       %s\n", jobsSummary)
	} else if o.jobsErr != nil {
		fmt.Fprintf(&b, "Jobs       not observed: %s\n", present.Error(o.jobsErr))
	}
	var entries []entry
	switch {
	case o.bookmarksErr != nil:
		fmt.Fprintf(&b, "Bookmarks  not read: %s\n", present.Error(o.bookmarksErr))
	case o.state.Status == "initialized" && !o.bookmarks.Enabled:
		b.WriteString("Bookmarks  not enabled; nothing about reading is recorded\n")
	case o.bookmarks.Enabled:
		fmt.Fprintf(&b, "Bookmarks  %d saved; only explicit bookmarks are kept\n", len(o.bookmarks.Bookmarks))
		for i, bm := range o.bookmarks.Bookmarks {
			if i == 5 {
				break
			}
			label := firstOf(present.Line(bm.Note), shortID(bm.AssetID))
			saved := bm
			entries = append(entries, entry{
				title:    "Continue: " + label,
				summary:  fmt.Sprintf("explicit bookmark, unit %d, offset %d", bm.Unit, bm.Offset),
				detail:   "Saved bookmark\n\n" + rows("Asset", present.Exact(bm.AssetID), "Representation", present.Line(bm.Representation), "Unit", fmt.Sprint(bm.Unit), "Offset", fmt.Sprint(bm.Offset), "Note", present.Line(bm.Note)) + "\nEnter opens this saved location in Read. Only explicit bookmarks are kept; nothing else about reading is recorded.\n",
				sourceID: bm.AssetID, identity: bm.AssetID, bookmark: &saved,
			})
		}
	}
	for _, s := range sections[1:] {
		summary := sectionUses[s.name]
		switch s.name {
		case "Library":
			if o.holdingsOK {
				summary = fmt.Sprintf("%d checked, %d in review; Enter lists them", o.checked, o.review)
			}
		case "Jobs":
			if jobsSummary != "" {
				summary = jobsSummary
			}
		}
		entries = append(entries, entry{title: s.name, summary: summary, detail: s.name + "\n\n" + sectionUses[s.name] + ".\n\nEnter goes there.\n", step: s.steps[0]})
	}
	b.WriteString("\nRead offline from this library's control files; nothing was written.\n")
	m.populate(nil)
	m.entries = entries
	m.selectEntry(0)
	m.setReport(b.String())
	m.status = "Library overview. Enter on an item goes there."
}

// homeEnter goes to the selected section or opens the selected bookmark.
func (m *model) homeEnter() tea.Cmd {
	e := m.entries[m.selected]
	dir := m.submittedInput
	if e.bookmark != nil {
		asset, representation := e.bookmark.AssetID, e.bookmark.Representation
		cmd := m.switchMode("read")
		m.reader = readerState{asset: asset, representation: representation}
		m.input.SetValue(dir)
		m.secondary.SetValue(dir)
		return tea.Batch(cmd, m.restoreBookmark())
	}
	step := e.step
	cmd := m.switchMode(step)
	if step == "holdings" && dir != "" && m.input.Value() == dir && len(m.entries) == 0 && m.report == "" {
		return tea.Batch(cmd, m.start(true))
	}
	if step == "state" && m.input.Value() == "" {
		m.status = "Enter an existing folder, then F7 reviews initialization."
	}
	return cmd
}

// shortID shortens an identity for cards and lists: the first 12 characters
// after the scheme, "...", and the last 4. The full value stays in F5.
func shortID(id string) string {
	scheme, value, ok := strings.Cut(id, ":")
	if !ok {
		scheme, value = "", id
	} else {
		scheme += ":"
	}
	if len(value) <= 20 {
		return present.Line(id)
	}
	return present.Line(scheme + value[:12] + "..." + value[len(value)-4:])
}
