package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/acquisition"
	"github.com/blisspixel/nemalo/internal/transfer"
)

func TestResumeWordingMatchesTheOutcome(t *testing.T) {
	if s := Acquisition(acquisition.Result{ResumedFrom: 10, Complete: true}); !strings.Contains(s, "whole file was verified") {
		t.Fatal(s)
	}
	if s := Acquisition(acquisition.Result{ResumedFrom: 10}); strings.Contains(s, "verified") || !strings.Contains(s, "did not complete") {
		t.Fatal(s)
	}
	early := Acquisition(acquisition.Result{Output: "packet", Status: "retained_incomplete"})
	if strings.Contains(early, "--resume") || !strings.Contains(early, "start a new output") || !strings.Contains(early, "no transfer record") {
		t.Fatal(early)
	}
	partial := Acquisition(acquisition.Result{Output: "packet", Status: "retained_incomplete", TransferRecorded: true, Transfer: transfer.Result{Bytes: 40}})
	if !strings.Contains(partial, "--resume") || strings.Contains(partial, "start a new output") {
		t.Fatal(partial)
	}
}
