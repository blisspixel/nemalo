package scanner

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// StaleAfter is the signature age at which doctor warns. Both default engines
// normally update several times a day, so a week means updates are not running.
const StaleAfter = 7 * 24 * time.Hour

// Signatures describes installed definitions without running a scanner. A
// definition date shows when signatures were built or applied, not that they
// detect any particular threat.
type Signatures struct {
	Tool    string     `json:"tool"`
	Status  string     `json:"status"`
	Version string     `json:"version,omitempty"`
	Updated *time.Time `json:"updated,omitempty"`
	AgeDays *int       `json:"age_days,omitempty"`
	Source  string     `json:"source,omitempty"`
	Advice  string     `json:"advice,omitempty"`
}

// Status values: current, stale, unknown, and unavailable (no definitions found).
func classifyAge(s *Signatures, updated time.Time, now time.Time) {
	u := updated.UTC()
	s.Updated = &u
	days := max(0, int(now.Sub(updated)/(24*time.Hour)))
	s.AgeDays = &days
	s.Status = "current"
	if now.Sub(updated) > StaleAfter {
		s.Status = "stale"
	}
}

// ClamAV database locations: the freshclam.conf DatabaseDirectory when one of
// the standard configuration files sets it, otherwise distribution defaults
// (Arch, Debian, Ubuntu, Fedora), Homebrew on Apple Silicon and Intel, then a
// source build.
var (
	freshclamConfigs = []string{"/etc/clamav/freshclam.conf", "/etc/freshclam.conf", "/opt/homebrew/etc/clamav/freshclam.conf", "/usr/local/etc/clamav/freshclam.conf", "/usr/local/etc/freshclam.conf"}
	clamDatabaseDirs = []string{"/var/lib/clamav", "/opt/homebrew/var/lib/clamav", "/usr/local/var/lib/clamav", "/usr/local/share/clamav"}
)

// ClamAVSignatures reads the daily database header. It never executes ClamAV.
func ClamAVSignatures(configs, dirs []string, now time.Time) Signatures {
	s := Signatures{Tool: "clamav", Status: "unavailable", Advice: "Run freshclam, or enable the clamav-freshclam service, to download and update signatures."}
	candidates := []string{}
	for _, config := range configs {
		if dir := databaseDirectory(config); dir != "" {
			candidates = append(candidates, dir)
			break
		}
	}
	candidates = append(candidates, dirs...)
	var best *cvdHeader
	for _, dir := range candidates {
		for _, name := range []string{"daily.cld", "daily.cvd"} {
			path := filepath.Join(dir, name)
			h, err := readCVDHeader(path)
			if err != nil {
				continue
			}
			if best == nil || h.built.After(best.built) {
				best = &h
			}
		}
		if best != nil {
			break
		}
	}
	if best == nil {
		return s
	}
	s.Version, s.Source = best.version, best.path
	classifyAge(&s, best.built, now)
	s.Advice = ""
	if s.Status == "stale" {
		s.Advice = "Signatures are more than 7 days old. Run freshclam, or enable the clamav-freshclam service (systemctl enable --now clamav-freshclam.service on systemd distributions)."
	}
	return s
}

// databaseDirectory returns the DatabaseDirectory value from a freshclam.conf.
func databaseDirectory(config string) string {
	f, err := os.Open(config)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, 1<<20))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "DatabaseDirectory" && filepath.IsAbs(fields[1]) {
			return fields[1]
		}
	}
	return ""
}

type cvdHeader struct {
	path, version string
	built         time.Time
}

// readCVDHeader parses the 512-byte header shared by .cvd and .cld databases:
// ClamAV-VDB:build time:version:signatures:functionality level:md5:
// digital signature:builder:creation time in Unix seconds. freshclam rewrites
// the .cld header as it applies incremental updates.
func readCVDHeader(path string) (cvdHeader, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 512 {
		return cvdHeader{}, errors.New("no database header")
	}
	f, err := os.Open(path)
	if err != nil {
		return cvdHeader{}, err
	}
	defer f.Close()
	buf := make([]byte, 512)
	if _, err := io.ReadFull(f, buf); err != nil {
		return cvdHeader{}, err
	}
	return parseCVDHeader(path, string(buf))
}

func parseCVDHeader(path, header string) (cvdHeader, error) {
	fields := strings.Split(strings.TrimRight(header, " \x00\n"), ":")
	if len(fields) < 4 || fields[0] != "ClamAV-VDB" {
		return cvdHeader{}, errors.New("not a ClamAV database header")
	}
	h := cvdHeader{path: path, version: strings.TrimSpace(fields[2])}
	if _, err := strconv.Atoi(h.version); err != nil {
		return cvdHeader{}, errors.New("invalid database version")
	}
	if len(fields) >= 9 {
		if seconds, err := strconv.ParseInt(strings.TrimSpace(fields[8]), 10, 64); err == nil && seconds > 0 {
			h.built = time.Unix(seconds, 0)
			return h, nil
		}
	}
	// Older headers carry only the formatted build time.
	built, err := time.Parse("02 Jan 2006 15-04 -0700", strings.TrimSpace(fields[1]))
	if err != nil {
		return cvdHeader{}, errors.New("database header has no build time")
	}
	h.built = built
	return h, nil
}
