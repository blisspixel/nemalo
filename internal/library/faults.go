package library

import "io"

// writeFault wraps the writer of one named write so tests can fail it partway
// the way a full disk does: "import copy", "journal", "cleanup copy", and
// "export copy". It returns the writer unchanged outside tests.
var writeFault = func(stage string, w io.Writer) io.Writer { return w }
