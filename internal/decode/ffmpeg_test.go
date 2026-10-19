package decode

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMain lets this test binary stand in for ffmpeg and ffprobe: with
// NEMALO_FAKE_FFMPEG set it answers as the mode names. Every call must read
// only through the file protocol and a fixed demuxer.
func TestMain(m *testing.M) {
	mode, ok := os.LookupEnv("NEMALO_FAKE_FFMPEG")
	if !ok {
		os.Exit(m.Run())
	}
	args := os.Args[1:]
	if slices.Contains(args, "-version") {
		os.Stdout.WriteString("ffmpeg version 9.9-test Copyright\n")
		os.Exit(0)
	}
	whitelist := slices.Index(args, "-protocol_whitelist")
	format := slices.Index(args, "-f")
	input := ""
	for _, a := range args {
		if strings.HasPrefix(a, "file:") {
			input = a
		}
	}
	if whitelist < 0 || args[whitelist+1] != "file" || format < 0 || args[format+1] != os.Getenv("NEMALO_FAKE_DEMUXER") || input == "" {
		os.Stderr.WriteString("unsafe arguments\n")
		os.Exit(9)
	}
	if slices.Contains(args, "-show_entries") {
		if mode != "nostream" {
			os.Stdout.WriteString("sample_rate=1000\nchannels=2\n")
		}
		os.Exit(0)
	}
	switch mode {
	case "clean":
		os.Stdout.Write(make([]byte, 2*1500)) // 1500 samples at 1000 Hz
	case "errors":
		os.Stdout.Write(make([]byte, 2*900))
		os.Stderr.WriteString("[aac @ 0x1] Error decoding AAC frame header.\n    Last message repeated 4 times\n")
	case "odd":
		os.Stdout.Write(make([]byte, 3))
	case "fail":
		os.Stderr.WriteString("Error opening input\n")
		os.Exit(1)
	case "hang":
		time.Sleep(time.Hour)
	}
	os.Exit(0)
}

func fake(t *testing.T, mode, demuxer string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	saved := LookPath
	LookPath = func(string) (string, error) { return exe, nil }
	t.Cleanup(func() { LookPath = saved })
	t.Setenv("NEMALO_FAKE_FFMPEG", mode)
	t.Setenv("NEMALO_FAKE_DEMUXER", demuxer)
}

func TestDecodeCountsSamplesAndReportsErrors(t *testing.T) {
	ctx := context.Background()
	fake(t, "clean", "mov")
	r := Audio(ctx, "/private/asset.m4b", "m4b", 10)
	if r.Status != "passed" || r.SampleRate != 1000 || r.Channels != 2 || r.DecodedSamples != 1500 || r.DecodedDurationMs != 1500 || r.Version != "9.9-test" || r.Errors != 0 {
		t.Fatalf("%+v", r)
	}
	fake(t, "errors", "mp3")
	r = Audio(ctx, "/private/asset.mp3", "mp3", 10)
	if r.Status != "errors_reported" || r.Errors != 5 || len(r.Messages) != 1 || r.DecodedDurationMs != 900 {
		t.Fatalf("%+v", r)
	}
	for mode, detail := range map[string]string{"odd": "partial sample", "fail": "decoded no audio", "nostream": "no audio stream"} {
		fake(t, mode, "mov")
		if r := Audio(ctx, "/private/asset.m4a", "m4a", 10); r.Status != "incomplete" || !strings.Contains(r.Detail, detail) {
			t.Errorf("%s: %+v", mode, r)
		}
	}
	// The demuxer is fixed by format; a format without one is not decoded.
	if r := Audio(ctx, "/private/asset.epub", "epub", 10); r.Status != "incomplete" || Supported("epub") || !Supported("m4b") {
		t.Fatalf("%+v", r)
	}
}

func TestDecodeIsUnavailableOrStopsAtItsDeadline(t *testing.T) {
	saved := LookPath
	t.Cleanup(func() { LookPath = saved })
	LookPath = func(name string) (string, error) {
		if name == "ffprobe" {
			return "", errors.New("missing")
		}
		return "/usr/bin/true", nil
	}
	if r := Audio(context.Background(), "/private/asset.mp3", "mp3", 10); r.Status != "unavailable" || !strings.Contains(r.Detail, "ffprobe") {
		t.Fatalf("%+v", r)
	}
	fake(t, "hang", "mp3")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	if r := Audio(ctx, "/private/asset.mp3", "mp3", 10); r.Status != "incomplete" || time.Since(started) > 10*time.Second {
		t.Fatalf("%+v after %v", r, time.Since(started))
	}
	if Deadline(0) != 2*time.Minute || Deadline(2<<30) != 34*time.Minute || Deadline(1<<40) != time.Hour {
		t.Fatal(Deadline(0), Deadline(2<<30))
	}
}

func TestToolTextIsBoundedAndSingleLine(t *testing.T) {
	if got := textField("a\x1b[31m\nb\x00c", 100); got != "a [31m b c" {
		t.Fatalf("%q", got)
	}
	if got := textField(strings.Repeat("é", 10), 5); got != "éé" {
		t.Fatalf("%q", got)
	}
}
