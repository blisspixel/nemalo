package pdftext

import (
	"context"
	"sort"

	"github.com/digitorus/pdf"
)

// Active content is anything a PDF reader may run, open, fetch, or extract
// without the reader choosing to: scripts, launched programs, automatic
// actions, attached files, form submission, remote documents, rich media,
// and XFA forms. A web link opens only when followed, so links are counted,
// not reported as findings.
const (
	activeJavaScript = "pdf_javascript"
	activeLaunch     = "pdf_launch_action"
	activeAutomatic  = "pdf_automatic_action"
	activeEmbedded   = "pdf_embedded_file"
	activeSubmit     = "pdf_form_submission"
	activeRemote     = "pdf_remote_reference"
	activeRichMedia  = "pdf_rich_media"
	activeXFA        = "pdf_xfa_form"
	activeOverBudget = "pdf_active_content_not_fully_examined"
)

// activeBudget bounds the values one walk visits; activeDepth bounds nesting.
const (
	activeBudget = 500_000
	activeDepth  = 64
)

// activeWalk visits every object reachable from the trailer once, reading
// dictionary keys and array items but never decoding stream data.
type activeWalk struct {
	ctx      context.Context
	seen     map[pdf.Ptr]bool
	found    map[string]bool
	left     int
	links    int
	complete bool
}

// scanActive returns the active-content findings, the number of web links,
// and whether every reachable object was examined.
func scanActive(ctx context.Context, trailer pdf.Value) (findings []string, links int, complete bool) {
	w := &activeWalk{ctx: ctx, seen: map[pdf.Ptr]bool{}, found: map[string]bool{}, left: activeBudget, complete: true}
	w.value(trailer, pdf.Ptr{}, 0)
	if !w.complete {
		w.found[activeOverBudget] = true
	}
	for f := range w.found {
		findings = append(findings, f)
	}
	sort.Strings(findings)
	return findings, w.links, w.complete
}

func (w *activeWalk) value(v pdf.Value, parent pdf.Ptr, depth int) {
	if !w.complete {
		return
	}
	w.left--
	if w.left < 0 || depth > activeDepth || w.ctx.Err() != nil {
		w.complete = false
		return
	}
	switch v.Kind() {
	case pdf.Dict, pdf.Stream:
	case pdf.Array:
		if p := v.GetPtr(); p != parent {
			if w.seen[p] {
				return
			}
			w.seen[p] = true
			parent = p
		}
		for i := 0; i < v.Len(); i++ {
			w.value(v.Index(i), parent, depth+1)
		}
		return
	default:
		return
	}
	// An indirect object is examined once, however often it is referenced.
	if p := v.GetPtr(); p != parent {
		if w.seen[p] {
			return
		}
		w.seen[p] = true
		parent = p
	}
	w.classify(v)
	for _, key := range v.Keys() {
		if key == "Parent" || key == "P" {
			// Back references to an ancestor add nothing new.
			continue
		}
		w.value(v.Key(key), parent, depth+1)
	}
}

func (w *activeWalk) classify(d pdf.Value) {
	for _, key := range d.Keys() {
		switch key {
		case "JS", "JavaScript":
			w.found[activeJavaScript] = true
		case "AA":
			w.found[activeAutomatic] = true
		case "OpenAction":
			// Opening at a page is ordinary; an action on open is not.
			if a := d.Key("OpenAction"); a.Kind() == pdf.Dict && a.Key("S").Name() != "GoTo" {
				w.found[activeAutomatic] = true
			}
		case "EmbeddedFile", "EmbeddedFiles", "EF":
			w.found[activeEmbedded] = true
		case "RichMedia", "RichMediaContent":
			w.found[activeRichMedia] = true
		case "XFA":
			w.found[activeXFA] = true
		}
	}
	switch d.Key("S").Name() {
	case "JavaScript":
		w.found[activeJavaScript] = true
	case "Launch":
		w.found[activeLaunch] = true
	case "SubmitForm", "ImportData":
		w.found[activeSubmit] = true
	case "GoToR", "GoToE":
		w.found[activeRemote] = true
	case "Movie", "Sound", "Rendition", "RichMediaExecute":
		w.found[activeRichMedia] = true
	case "URI":
		w.links++
	}
	switch d.Key("Subtype").Name() {
	case "FileAttachment":
		w.found[activeEmbedded] = true
	case "RichMedia", "Movie", "Sound", "Screen", "3D":
		w.found[activeRichMedia] = true
	}
}
