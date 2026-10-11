package scanner

import (
	"bufio"
	"io"
	"os"
	"slices"
	"strings"
	"time"
)

// SetupHint names the platform command that installs the default scanner. It
// is guidance only: Nemalo never installs software or changes host policy.
// osRelease is the content of /etc/os-release on Linux and ignored elsewhere.
func SetupHint(goos, osRelease string) string {
	switch goos {
	case "windows":
		return "Turn on Microsoft Defender Antivirus in Windows Security; Nemalo uses its MpCmdRun.exe scanner."
	case "darwin":
		return "Install ClamAV to scan: brew install clamav, create $(brew --prefix)/etc/clamav/freshclam.conf from freshclam.conf.sample with the Example line removed, then run freshclam."
	}
	family := linuxFamily(osRelease)
	switch family {
	case "arch":
		return "Install ClamAV to scan: sudo pacman -S clamav && sudo freshclam, then sudo systemctl enable --now clamav-freshclam.service to keep signatures current."
	case "debian":
		return "Install ClamAV to scan: sudo apt install clamav clamav-freshclam. The clamav-freshclam service downloads and updates signatures."
	case "fedora":
		return "Install ClamAV to scan: sudo dnf install clamav clamav-freshclam && sudo freshclam, then sudo systemctl enable --now clamav-freshclam.service."
	case "suse":
		return "Install ClamAV to scan: sudo zypper install clamav && sudo freshclam."
	case "alpine":
		return "Install ClamAV to scan: sudo apk add clamav && sudo freshclam."
	}
	return "Install ClamAV to scan: install your distribution's clamav package so clamscan is on PATH, then run freshclam to download signatures."
}

// linuxFamily maps os-release ID and ID_LIKE to a package-manager family.
// Derivatives such as Omarchy, Manjaro, Ubuntu, and Rocky declare their parent
// in ID_LIKE, so both fields are considered.
func linuxFamily(osRelease string) string {
	ids := []string{}
	scanner := bufio.NewScanner(strings.NewReader(osRelease))
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok || (key != "ID" && key != "ID_LIKE") {
			continue
		}
		for _, id := range strings.Fields(strings.ToLower(strings.Trim(value, `"'`))) {
			if strings.HasPrefix(id, "opensuse") {
				id = "opensuse" // opensuse-tumbleweed, opensuse-leap
			}
			ids = append(ids, id)
		}
	}
	families := []struct {
		name string
		ids  []string
	}{
		{"arch", []string{"arch", "archarm"}},
		{"debian", []string{"debian", "ubuntu"}},
		{"fedora", []string{"fedora", "rhel", "centos"}},
		{"suse", []string{"suse", "opensuse", "sles"}},
		{"alpine", []string{"alpine"}},
	}
	// The first recognized identifier, in file order, selects the family.
	for _, id := range ids {
		for _, family := range families {
			if slices.Contains(family.ids, id) {
				return family.name
			}
		}
	}
	return ""
}

// HostSetupHint reads /etc/os-release on Linux, bounded, and returns SetupHint.
func HostSetupHint(goos string) string {
	release := ""
	if goos == "linux" {
		if f, err := os.Open("/etc/os-release"); err == nil {
			b, _ := io.ReadAll(io.LimitReader(f, 64<<10))
			_ = f.Close()
			release = string(b)
		}
	}
	return SetupHint(goos, release)
}

// HostSignatures reports definitions for the platform's default scanner.
func HostSignatures(goos string, now time.Time) Signatures {
	if goos == "windows" {
		return DefenderSignatures(now)
	}
	return ClamAVSignatures(freshclamConfigs, clamDatabaseDirs, now)
}
