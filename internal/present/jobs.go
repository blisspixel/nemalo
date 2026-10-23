package present

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/jobs"
)

// JobSnapshot displays recorded facts without implying a live owner check.
func JobSnapshot(r app.JobSnapshotResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Job observation: %s\n", words(r.Status))
	field(&b, "Library identity", Line(r.LibraryID))
	field(&b, "Catalog observation", words(r.Catalog))
	field(&b, "Relationships", words(r.Coverage))
	fmt.Fprintf(&b, "Jobs: %d\n", len(r.Jobs))
	for _, j := range r.Jobs {
		fmt.Fprintf(&b, "%s %s, recorded %s, owner %s, %s\n", Line(j.ID), Line(j.Kind), words(j.RecordedStatus), words(j.Owner), words(j.Relationship))
	}
	if r.Truncated {
		b.WriteString("Result limit reached; only the newest observed matches are shown.\n")
	}
	for _, p := range r.Work.Problems {
		fmt.Fprintf(&b, "Incomplete: %s\n", words(p))
	}
	for _, limitation := range r.Limitations {
		fmt.Fprintf(&b, "%s\n", Line(limitation))
	}
	return b.String()
}

// Jobs lists durable jobs, one line each.
func Jobs(list []jobs.Job) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Jobs: %d\n", len(list))
	for _, j := range list {
		fmt.Fprintf(&b, "%s %s %s, started %s%s\n", Line(j.ID), Line(j.Kind), words(j.Status), Line(j.Created), progress(j))
	}
	return b.String()
}

// Job shows one durable job with its request, progress, and outcome.
func Job(j jobs.Job) string {
	var b strings.Builder
	field(&b, "Job", Line(j.ID))
	field(&b, "Kind", Line(j.Kind))
	field(&b, "Status", words(j.Status))
	if j.Status == jobs.Queued || j.Status == jobs.Running {
		field(&b, "Run by", words(j.Owner))
	}
	field(&b, "Started", Line(j.Created))
	field(&b, "Updated", Line(j.Updated))
	if p := strings.TrimPrefix(progress(j), ", "); p != "" {
		field(&b, "Progress", p)
	}
	if r, err := json.Marshal(j.Request); err == nil && j.Request != nil {
		field(&b, "Request", Line(string(r)))
	}
	field(&b, "Error", Line(j.Error))
	field(&b, "Detail", Line(j.Detail))
	if j.Result != nil {
		b.WriteString("Result is recorded; see --json for all of it.\n")
	}
	return b.String()
}

// progress is a job's last reported step, while it runs or for how far an
// interrupted or cancelled one got; a succeeded or failed job has its result.
func progress(j jobs.Job) string {
	p := j.Progress
	switch {
	case p == nil || j.Status == jobs.Succeeded || j.Status == jobs.Failed:
		return ""
	case p.Total > 0:
		return fmt.Sprintf(", %s %d of %d %s", words(p.Phase), p.Done, p.Total, Line(p.Unit))
	case p.Done > 0:
		return fmt.Sprintf(", %s %d %s", words(p.Phase), p.Done, Line(p.Unit))
	}
	return ", " + words(p.Phase)
}
