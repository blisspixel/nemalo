//go:build windows

package scanner

import (
	"encoding/binary"
	"time"

	"golang.org/x/sys/windows/registry"
)

// DefenderSignatures reads the definition version and apply time Defender
// records in the registry. These values are readable without elevation but are
// not a documented interface; Get-MpComputerStatus is the documented route and
// is not run here. Unreadable or unexpected values are reported as unknown.
func DefenderSignatures(now time.Time) Signatures {
	s := Signatures{Tool: "defender", Status: "unknown", Source: `HKLM\SOFTWARE\Microsoft\Windows Defender\Signature Updates (undocumented values)`}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows Defender\Signature Updates`, registry.QUERY_VALUE)
	if err != nil {
		s.Advice = "Defender definition details are unavailable. Windows Security shows the current definition version."
		return s
	}
	defer key.Close()
	if version, _, err := key.GetStringValue("AVSignatureVersion"); err == nil && len(version) <= 64 {
		s.Version = version
	}
	if raw, kind, err := key.GetBinaryValue("AVSignatureApplied"); err == nil && kind == registry.BINARY && len(raw) == 8 {
		if applied, ok := fileTime(binary.LittleEndian.Uint64(raw)); ok && !applied.After(now.Add(24*time.Hour)) {
			classifyAge(&s, applied, now)
			if s.Status == "stale" {
				s.Advice = "Definitions are more than 7 days old. Update them in Windows Security or with Windows Update."
			}
		}
	}
	return s
}

// fileTime converts a Windows FILETIME (100 ns intervals since 1601) to UTC.
func fileTime(ticks uint64) (time.Time, bool) {
	const unixEpoch = 116444736000000000
	if ticks <= unixEpoch {
		return time.Time{}, false
	}
	return time.Unix(0, int64(ticks-unixEpoch)*100).UTC(), true
}
