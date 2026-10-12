package present

import (
	"strings"
	"testing"

	"github.com/blisspixel/nemalo/internal/acquisition"
)

func TestResumeWordingMatchesTheOutcome(t *testing.T) {
	if s := Acquisition(acquisition.Result{ResumedFrom: 10, Complete: true}); !strings.Contains(s, "whole file was verified") {
		t.Fatal(s)
	}
	if s := Acquisition(acquisition.Result{ResumedFrom: 10}); strings.Contains(s, "verified") || !strings.Contains(s, "did not complete") {
		t.Fatal(s)
	}
}
