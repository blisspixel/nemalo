package tui

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestOmarchyTerminalsKeepColor(t *testing.T) {
	for _, term := range []string{"foot", "xterm-ghostty", "alacritty", "xterm-kitty"} {
		if got := colorprofile.Env([]string{"TERM=" + term}); got.String() != "TrueColor" {
			t.Fatalf("%s profile %s", term, got)
		}
	}
	if got := colorprofile.Env([]string{"TERM=tmux-256color", "COLORTERM=truecolor"}); got.String() != "ANSI256" {
		t.Fatalf("tmux profile %s", got)
	}
	if got := colorprofile.Env([]string{"TERM=dumb", "COLORTERM=truecolor"}); got.String() == "TrueColor" {
		t.Fatal(got)
	}
}
