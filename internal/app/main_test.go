package app

import (
	"os"
	"testing"

	"github.com/blisspixel/nemalo/internal/tempdirtest"
)

// TestMain removes the acquisition output locks the tests leave behind.
func TestMain(m *testing.M) {
	os.Exit(tempdirtest.Run(m))
}
