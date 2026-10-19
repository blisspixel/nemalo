package conformance

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain lets this test binary stand in for epubcheck: with NEMALO_FAKE_EPUBCHECK
// set it writes that report, or misbehaves as named, to the --json path.
func TestMain(m *testing.M) {
	mode, ok := os.LookupEnv("NEMALO_FAKE_EPUBCHECK")
	if !ok {
		os.Exit(m.Run())
	}
	args := os.Args[1:]
	if len(args) != 3 || args[1] != "--json" || !strings.HasSuffix(args[0], ".epub") {
		os.Exit(2)
	}
	switch mode {
	case "hang":
		time.Sleep(time.Hour)
	case "none":
		os.Exit(1)
	default:
		_ = os.WriteFile(args[2], []byte(mode), 0o600)
		if strings.Contains(mode, `"nError":0`) {
			os.Exit(0)
		}
		os.Exit(1)
	}
}

func fake(t *testing.T, mode string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	saved := LookPath
	LookPath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { LookPath = saved })
	t.Setenv("NEMALO_FAKE_EPUBCHECK", mode)
}

func report(fatal, errs, warnings int, messages string) string {
	return `{"checker":{"checkerVersion":"5.3.0","nFatal":` + strconv.Itoa(fatal) + `,"nError":` + strconv.Itoa(errs) + `,"nWarning":` + strconv.Itoa(warnings) + `,"nUsage":0},"messages":[` + messages + `]}`
}

const rscError = `{"ID":"RSC-005","severity":"ERROR","message":"Error while parsing file: element \"x\" not allowed","locations":[{"path":"OEBPS/c1.xhtml","line":12,"column":4}]}`
const opfWarning = `{"ID":"OPF-085","severity":"WARNING","message":"dc:identifier is not a valid UUID"}`

func book(t *testing.T) string {
	p := t.TempDir() + "/asset.epub"
	if err := os.WriteFile(p, []byte("PK"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEPUBCheckReportsAreReadNotTrustedBlindly(t *testing.T) {
	ctx := context.Background()
	fake(t, report(0, 0, 1, opfWarning))
	if r := CheckEPUB(ctx, book(t)); r.Status != "passed" || r.Version != "5.3.0" || r.Warnings != 1 || len(r.Messages) != 1 || r.Messages[0].ID != "OPF-085" {
		t.Fatal(r)
	}
	fake(t, report(0, 1, 0, rscError))
	r := CheckEPUB(ctx, book(t))
	if r.Status != "errors_reported" || r.Errors != 1 || r.Messages[0].Locations[0] != (Location{Path: "OEBPS/c1.xhtml", Line: 12, Column: 4}) {
		t.Fatal(r)
	}
	for name, mode := range map[string]string{
		"no report":         "none",
		"not json":          "{",
		"no checker":        `{"messages":[]}`,
		"no version":        `{"checker":{"nFatal":0,"nError":0,"nWarning":0,"nUsage":0},"messages":[]}`,
		"no messages":       `{"checker":{"checkerVersion":"5","nFatal":0,"nError":0,"nWarning":0,"nUsage":0}}`,
		"hidden error":      report(0, 0, 0, rscError),
		"uncounted listing": report(0, 2, 0, opfWarning),
		"negative":          `{"checker":{"checkerVersion":"5","nFatal":-1,"nError":0,"nWarning":0,"nUsage":0},"messages":[]}`,
		"odd severity":      report(0, 0, 0, `{"ID":"X","severity":"FINE","message":"m"}`),
	} {
		fake(t, mode)
		if r := CheckEPUB(ctx, book(t)); r.Status != "incomplete" || r.Detail == "" {
			t.Fatal(name, r)
		}
	}
	saved := LookPath
	LookPath = func() (string, error) { return "", errors.New("not found") }
	if r := CheckEPUB(ctx, book(t)); r.Status != "unavailable" {
		t.Fatal(r)
	}
	LookPath = saved
}

func TestEPUBCheckIsBoundedAndCancellable(t *testing.T) {
	fake(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if r := CheckEPUB(ctx, book(t)); r.Status != "incomplete" || time.Since(start) > 10*time.Second {
		t.Fatal(r, time.Since(start))
	}
	many := strings.TrimSuffix(strings.Repeat(opfWarning+",", maxMessages+5), ",")
	fake(t, `{"checker":{"checkerVersion":"5","nFatal":0,"nError":0,"nWarning":205,"nUsage":0},"messages":[`+many+`]}`)
	if r := CheckEPUB(context.Background(), book(t)); r.Status != "passed" || len(r.Messages) != maxMessages || r.MessagesOmitted != 5 {
		t.Fatal(len(r.Messages), r.MessagesOmitted)
	}
	if got := textField("a\x00b\nc"+strings.Repeat("é", 10), 6); got != "a b c" && !strings.HasPrefix(got, "a b c") {
		t.Fatal(got)
	}
}
