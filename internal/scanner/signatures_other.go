//go:build !windows

package scanner

import "time"

// DefenderSignatures is Windows-only; other platforms report it unavailable.
func DefenderSignatures(time.Time) Signatures {
	return Signatures{Tool: "defender", Status: "unavailable"}
}
