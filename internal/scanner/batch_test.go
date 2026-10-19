package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func clamBatch(output string, code int, truncated bool, err error, calls *int) Adapter {
	return Adapter{Resolve: func() (string, string, error) { return "clamav", "clamscan", nil }, Run: func(_ context.Context, exe string, args []string) (string, int, bool, error) {
		*calls++
		joined := strings.Join(args, " ")
		if exe != "clamscan" || !strings.Contains(joined, "--alert-exceeds-max=yes") || !strings.HasSuffix(joined, " -- /s/a.epub /s/b.epub") {
			return "", -1, false, errors.New("unexpected invocation " + joined)
		}
		return output, code, truncated, err
	}}
}

const summary2 = "\n----------- SCAN SUMMARY -----------\nKnown viruses: 3627000\nEngine version: 1.5.4\nScanned directories: 0\nScanned files: 2\nInfected files: %d\nData scanned: 0.01 MB\n"

func TestClamAVBatchLoadsSignaturesOnceAndAttributesEachFile(t *testing.T) {
	files := []string{"/s/a.epub", "/s/b.epub"}
	for _, tc := range []struct {
		name, output string
		code         int
		truncated    bool
		err          error
		want         [2]string
	}{
		{"both clean", "/s/a.epub: OK\n/s/b.epub: OK" + strings.Replace(summary2, "%d", "0", 1), 0, false, nil, [2]string{"no_detections_reported", "no_detections_reported"}},
		{"one detection", "/s/a.epub: OK\n/s/b.epub: Win.Test.EICAR_HDB-1 FOUND" + strings.Replace(summary2, "%d", "1", 1), 1, false, errors.New("exit 1"), [2]string{"no_detections_reported", "findings"}},
		{"per-file error", "/s/a.epub: OK\n/s/b.epub: Can't read file ERROR" + strings.Replace(summary2, "%d", "0", 1) + "Total errors: 1\n", 2, false, errors.New("exit 2"), [2]string{"incomplete", "incomplete"}},
		{"global warning", "LibClamAV Warning: limit reached\n/s/a.epub: OK\n/s/b.epub: OK" + strings.Replace(summary2, "%d", "0", 1), 0, false, nil, [2]string{"incomplete", "incomplete"}},
		{"missing line", "/s/a.epub: OK" + strings.Replace(summary2, "%d", "0", 1), 0, false, nil, [2]string{"incomplete", "incomplete"}},
		{"duplicate line", "/s/a.epub: OK\n/s/a.epub: OK\n/s/b.epub: OK" + strings.Replace(summary2, "%d", "0", 1), 0, false, nil, [2]string{"incomplete", "incomplete"}},
		{"no summary", "/s/a.epub: OK\n/s/b.epub: OK\n", 0, false, nil, [2]string{"incomplete", "incomplete"}},
		{"summary miscount", "/s/a.epub: OK\n/s/b.epub: OK" + strings.Replace(strings.Replace(summary2, "Scanned files: 2", "Scanned files: 1", 1), "%d", "0", 1), 0, false, nil, [2]string{"incomplete", "incomplete"}},
		{"truncated keeps detection", "/s/b.epub: Eicar FOUND\n/s/a.epub: OK", 1, true, errors.New("exit 1"), [2]string{"incomplete", "findings"}},
		{"crlf output", strings.ReplaceAll("/s/a.epub: OK\n/s/b.epub: OK"+strings.Replace(summary2, "%d", "0", 1), "\n", "\r\n"), 0, false, nil, [2]string{"no_detections_reported", "no_detections_reported"}},
		{"exit code disagrees", "/s/a.epub: OK\n/s/b.epub: OK" + strings.Replace(summary2, "%d", "0", 1), 1, false, errors.New("exit 1"), [2]string{"incomplete", "incomplete"}},
		{"limit alert is not scanned", "/s/a.epub: Heuristics.Limits.Exceeded.MaxFileSize FOUND\n/s/b.epub: OK" + strings.Replace(summary2, "%d", "1", 1), 1, false, errors.New("exit 1"), [2]string{"incomplete", "no_detections_reported"}},
		{"limit alert beside a detection", "/s/a.epub: Heuristics.Limits.Exceeded.MaxScanSize FOUND\n/s/a.epub: Eicar FOUND\n/s/b.epub: OK" + strings.Replace(summary2, "%d", "1", 1), 1, false, errors.New("exit 1"), [2]string{"findings", "no_detections_reported"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			results := clamBatch(tc.output, tc.code, tc.truncated, tc.err, &calls).ScanMany(context.Background(), files)
			if calls != 1 || len(results) != 2 {
				t.Fatal("batch must invoke clamscan once", calls)
			}
			for i, r := range results {
				if r.Status != tc.want[i] {
					t.Fatalf("file %d: %s, want %s", i, r.Status, tc.want[i])
				}
				if strings.Contains(r.Output, files[1-i]+":") {
					t.Fatal("output attributed to the wrong file", r.Output)
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	for _, r := range clamBatch("/s/a.epub: OK\n/s/b.epub: OK"+strings.Replace(summary2, "%d", "0", 1), 0, false, nil, &calls).ScanMany(ctx, files) {
		if r.Status != "incomplete" {
			t.Fatal("cancelled batch reported clean", r)
		}
	}
}

func TestScanManyFallsBackToSingleScans(t *testing.T) {
	calls := 0
	defender := Adapter{Resolve: func() (string, string, error) { return "defender", "MpCmdRun.exe", nil }, Run: func(_ context.Context, _ string, args []string) (string, int, bool, error) {
		calls++
		if strings.Count(strings.Join(args, " "), "-File") != 1 {
			t.Fatal("Defender accepts one -File per invocation", args)
		}
		return "Scanning x found no threats.", 0, false, nil
	}}
	results := defender.ScanMany(context.Background(), []string{"a", "b", "c"})
	if calls != 3 || len(results) != 3 || results[2].Status != "no_detections_reported" {
		t.Fatal(calls, results)
	}
	missing := Adapter{Resolve: func() (string, string, error) { return "", "", errors.New("missing") }}
	for _, r := range missing.ScanMany(context.Background(), []string{"a", "b"}) {
		if r.Status != "unavailable" {
			t.Fatal(r)
		}
	}
	if kind, err := defender.Kind(); kind != "defender" || err != nil {
		t.Fatal(kind, err)
	}
}

func TestDefenderDetectionRecognition(t *testing.T) {
	detected := "Scan starting...\r\nScan finished.\r\nScanning C:\\t\\asset.epub found 1 threats.\r\n\r\n<===========================LIST OF DETECTED THREATS==========================>\r\n----------------------------- Threat information ------------------------------\r\nThreat                  : Virus:DOS/EICAR_Test_File\r\nResources               : 1 total\r\n    file                : C:\\t\\asset.epub\r\n"
	for _, tc := range []struct {
		output string
		code   int
		want   string
	}{
		{detected, 2, "findings"},
		{detected, 0, "incomplete"},
		{"[Failed][0x80070002] The system cannot find the file specified.", 2, "incomplete"},
		{"Scanning C:\\t\\asset.epub found 1 threats.", 2, "incomplete"},
		{"Threat : Something", 2, "incomplete"},
		{"Scanning C:\\t\\asset.epub found 0 threats.\nThreat : x", 2, "incomplete"},
		{detected + "[Failed] partial", 2, "incomplete"},
	} {
		a := Adapter{Resolve: func() (string, string, error) { return "defender", "MpCmdRun.exe", nil }, Run: func(context.Context, string, []string) (string, int, bool, error) {
			return tc.output, tc.code, false, errors.New("exit")
		}}
		if r := a.Scan(context.Background(), "C:\\t\\asset.epub"); r.Status != tc.want {
			t.Fatalf("%q code %d: %s", tc.output, tc.code, r.Status)
		}
	}
}

func TestClamAVSignatureHeaders(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	fresh := now.Add(-26 * time.Hour).Unix()
	pad := func(s string) []byte { return []byte(s + strings.Repeat(" ", 512-len(s))) }
	dir := t.TempDir()
	cld := "ClamAV-VDB:13 Jan 2027 10-00 +0000:27500:2090000:90:X:X:clamav:" + itoa(fresh)
	if err := os.WriteFile(filepath.Join(dir, "daily.cld"), pad(cld), 0600); err != nil {
		t.Fatal(err)
	}
	s := ClamAVSignatures(nil, []string{filepath.Join(dir, "missing"), dir}, now)
	if s.Status != "current" || s.Version != "27500" || s.AgeDays == nil || *s.AgeDays != 1 || s.Advice != "" || s.Source != filepath.Join(dir, "daily.cld") {
		t.Fatal(s)
	}
	// An older .cvd beside a newer .cld does not hide the newer header.
	old := "ClamAV-VDB:01 Jan 2026 10-00 +0000:27000:2000000:90:X:X:clamav:" + itoa(now.Add(-300*24*time.Hour).Unix())
	if err := os.WriteFile(filepath.Join(dir, "daily.cvd"), pad(old), 0600); err != nil {
		t.Fatal(err)
	}
	if s := ClamAVSignatures(nil, []string{dir}, now); s.Version != "27500" {
		t.Fatal("older database chosen", s)
	}
	stale := t.TempDir()
	if err := os.WriteFile(filepath.Join(stale, "daily.cvd"), pad(old), 0600); err != nil {
		t.Fatal(err)
	}
	if s := ClamAVSignatures(nil, []string{stale}, now); s.Status != "stale" || !strings.Contains(s.Advice, "clamav-freshclam") {
		t.Fatal(s)
	}
	// freshclam.conf DatabaseDirectory takes precedence over defaults.
	conf := filepath.Join(t.TempDir(), "freshclam.conf")
	if err := os.WriteFile(conf, []byte("# comment\nDatabaseDirectory "+dir+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if s := ClamAVSignatures([]string{conf}, []string{stale}, now); s.Version != "27500" {
		t.Fatal("configured directory ignored", s)
	}
	if s := ClamAVSignatures(nil, []string{t.TempDir()}, now); s.Status != "unavailable" || s.Advice == "" {
		t.Fatal(s)
	}
	for _, header := range []string{"", "NotClamAV:x:1:2", "ClamAV-VDB:x:notnumber:1", "ClamAV-VDB:bad date:12:1:2:3:4:5:notseconds"} {
		if _, err := parseCVDHeader("p", header); err == nil {
			t.Fatal("invalid header accepted", header)
		}
	}
	if h, err := parseCVDHeader("p", "ClamAV-VDB:14 Jul 2021 14-29 -0400:26231:3990000:90"); err != nil || h.built.UTC().Format(time.RFC3339) != "2021-07-14T18:29:00Z" {
		t.Fatal("old-format build time", h, err)
	}
	short := filepath.Join(t.TempDir(), "daily.cvd")
	if err := os.WriteFile(short, []byte("ClamAV-VDB:short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCVDHeader(short); err == nil {
		t.Fatal("short database accepted")
	}
	if HostSignatures("linux", now).Tool != "clamav" {
		t.Fatal("linux default scanner")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestSetupHintsByPlatform(t *testing.T) {
	for _, tc := range []struct{ goos, release, want string }{
		{"linux", "ID=arch\n", "pacman -S clamav"},
		{"linux", "NAME=\"Omarchy\"\nID=omarchy\nID_LIKE=arch\n", "pacman -S clamav"},
		{"linux", "ID=ubuntu\nID_LIKE=debian\n", "apt install clamav clamav-freshclam"},
		{"linux", "ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", "dnf install"},
		{"linux", "ID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n", "zypper install clamav"},
		{"linux", "ID=alpine\n", "apk add clamav"},
		{"linux", "ID=unknownos\n", "distribution's clamav package"},
		{"linux", "", "distribution's clamav package"},
		{"darwin", "", "brew install clamav"},
		{"windows", "", "Microsoft Defender"},
	} {
		if got := SetupHint(tc.goos, tc.release); !strings.Contains(got, tc.want) {
			t.Fatalf("%s %q: %s", tc.goos, tc.release, got)
		}
	}
	if HostSetupHint("plan9") == "" {
		t.Fatal("no fallback hint")
	}
}
