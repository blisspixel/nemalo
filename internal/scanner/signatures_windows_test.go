//go:build windows

package scanner

import (
	"testing"
	"time"
)

func TestFileTimeConversion(t *testing.T) {
	// 2024-01-01T00:00:00Z as a Windows FILETIME.
	got, ok := fileTime(133485408000000000)
	if !ok || !got.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal(got, ok)
	}
	for _, ticks := range []uint64{0, 116444736000000000} {
		if _, ok := fileTime(ticks); ok {
			t.Fatal("pre-1970 or zero FILETIME accepted", ticks)
		}
	}
}

func TestDefenderSignaturesNeverClaimMoreThanTheRegistryShows(t *testing.T) {
	s := DefenderSignatures(time.Now())
	if s.Tool != "defender" || (s.Status != "current" && s.Status != "stale" && s.Status != "unknown") {
		t.Fatal(s)
	}
	if (s.Status == "unknown") != (s.Updated == nil) {
		t.Fatal("age status without an applied time, or a time without a status", s)
	}
}
