// Command archiver is the synthetic reader used by offline archive tests.
package main

import (
	"os"

	"github.com/blisspixel/nemalo/internal/staging/stagingtest"
)

func main() {
	stagingtest.Main()
	os.Exit(7)
}
