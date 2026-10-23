package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/jobs"
)

func TestJobSnapshotDisplaysRecordedUncertaintyAndEscapesText(t *testing.T) {
	r := app.JobSnapshotResult{Status: "incomplete", LibraryID: "library:test", Catalog: "busy", Coverage: "identity_associations_only", Truncated: true, Jobs: []app.RecordedJob{{ID: "job:test\x1b[31m", Kind: "library_rescan", RecordedStatus: "running", Owner: "not_observed", Relationship: "declared_target"}}, Work: jobs.Observation{Problems: []string{"journal_incomplete"}}, Limitations: []string{"Recorded outcomes only."}}
	s := JobSnapshot(r)
	r.Started, r.Finished = "2026-10-23T12:00:00Z", "2026-10-23T12:00:01Z"
	interval := JobSnapshot(r)
	if !strings.Contains(interval, "Observation started: "+r.Started) || !strings.Contains(interval, "Observation finished: "+r.Finished) {
		t.Fatal("observation time hidden", interval)
	}
	for _, want := range []string{"Job observation: incomplete", "recorded running", "owner not observed", "Catalog observation: busy", "Result limit reached", "Incomplete: journal incomplete", "Recorded outcomes only."} {
		if !strings.Contains(s, want) {
			t.Fatal(want, s)
		}
	}
	if strings.ContainsRune(s, '\x1b') {
		t.Fatal("untrusted terminal escape was rendered", s)
	}
}
