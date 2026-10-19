package library

import (
	"os"
	"testing"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func TestMain(m *testing.M) {
	stagingtest.Main()
	os.Exit(m.Run())
}
