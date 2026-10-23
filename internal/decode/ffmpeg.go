// Package decode runs an installed FFmpeg to decode an audio snapshot in full
// and count the samples it produces. It is an optional adapter: Nemalo works
// without it, never downloads it, never treats a decode that did not run as a
// pass, and keeps decoded timing apart from probed and declared timing.
package decode

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Result is one decode. Status is "passed" (FFmpeg exited cleanly and
// reported no error), "errors_reported", "unavailable" (FFmpeg or FFprobe is
// not installed), or "incomplete" (it could not open the file, timed out, or
// produced output that cannot be counted). Decoded samples are per channel at
// SampleRate, after the decoder's own trimming of encoder delay and padding.
type Result struct {
	Tool              string   `json:"tool"`
	Status            string   `json:"status"`
	Executable        string   `json:"executable,omitempty"`
	Version           string   `json:"version,omitempty"`
	SampleRate        int      `json:"sample_rate,omitempty"`
	Channels          int      `json:"channels,omitempty"`
	DecodedSamples    int64    `json:"decoded_samples"`
	DecodedDurationMs int64    `json:"decoded_duration_ms"`
	Errors            int      `json:"errors"`
	Messages          []string `json:"messages"`
	MessagesOmitted   int      `json:"messages_omitted,omitempty"`
	Detail            string   `json:"detail,omitempty"`
}

const (
	maxMessages = 20
	maxOutput   = 64 << 10
)

// LookPath finds FFmpeg and FFprobe; tests replace it.
var LookPath = exec.LookPath

// demuxers fixes the input format per assessed format, so a file is never
// probed as another kind, such as a playlist that names other resources.
var demuxers = map[string]string{"mp3": "mp3", "m4b": "mov", "m4a": "mov"}

// Supported reports a format this adapter decodes.
func Supported(format string) bool { return demuxers[format] != "" }

// Deadline allows two minutes and a minute per 64 MiB of input, at most an
// hour. FFmpeg decoded a 380 MB AAC audiobook in about 21 seconds on a
// desktop computer.
func Deadline(bytes int64) time.Duration {
	return min(2*time.Minute+time.Duration(max(bytes, 0)>>26)*time.Minute, time.Hour)
}

// Audio decodes the first audio stream of file, which must be a private
// snapshot path. Only the file protocol is allowed, so FFmpeg reads nothing
// but that file and makes no network request.
func Audio(ctx context.Context, file, format string, size int64) Result {
	return measureAudio(ctx, file, format, size, nil)
}

// measureAudio keeps the installed-tool selection, probing, output transform
// and error accounting shared by checks and explicitly requested PCM access.
// A capture opts into the latter's stricter work and process allowances.
func measureAudio(ctx context.Context, file, format string, size int64, capture *pcmCapture) Result {
	r := Result{Tool: "ffmpeg", Status: "incomplete", Messages: []string{}}
	demuxer := demuxers[format]
	if demuxer == "" {
		r.Detail = "no decoder is selected for this format"
		return r
	}
	ffmpeg, err := LookPath("ffmpeg")
	if err != nil {
		r.Status, r.Detail = "unavailable", "ffmpeg is not on PATH"
		return r
	}
	ffprobe, err := LookPath("ffprobe")
	if err != nil {
		r.Status, r.Detail = "unavailable", "ffprobe is not on PATH"
		return r
	}
	r.Executable = ffmpeg
	ctx, cancel := context.WithTimeout(ctx, Deadline(size))
	defer cancel()
	runCommand := run
	if capture != nil {
		runCommand = capture.run
	}
	input := "file:" + file
	if out, _, err := runCommand(ctx, ffmpeg, io.Discard, "-hide_banner", "-version"); err == nil {
		if fields := strings.Fields(firstLine(out)); len(fields) >= 3 && fields[1] == "version" {
			r.Version = textField(fields[2], 100)
		}
	}
	if capture != nil && r.Version == "" {
		r.Detail = "ffmpeg version could not be established"
		return r
	}
	inputOptions := []string{"-protocol_whitelist", "file", "-f", demuxer}
	if capture != nil {
		inputOptions = append(inputOptions, "-enable_drefs", "0", "-use_absolute_path", "0")
	}
	// The decoder's output rate and channels, which a decoder may change from
	// the container's, for example with spectral band replication.
	probeArgs := append([]string{"-v", "error"}, inputOptions...)
	probeArgs = append(probeArgs, "-select_streams", "a:0", "-show_entries", "stream=sample_rate,channels", "-of", "default=noprint_wrappers=1", input)
	out, _, err := runCommand(ctx, ffprobe, io.Discard, probeArgs...)
	if ctx.Err() != nil {
		r.Detail = "ffprobe did not finish within the deadline"
		return r
	}
	if capture != nil && !validPCMProbe(out) {
		r.Detail = "ffprobe output has missing, duplicate or unsupported stream fields"
		return r
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		n, convErr := strconv.Atoi(value)
		switch {
		case convErr != nil:
		case key == "sample_rate" && n > 0 && n <= 1_000_000:
			r.SampleRate = n
		case key == "channels" && n > 0 && n <= 64:
			r.Channels = n
		}
	}
	if err != nil || r.SampleRate == 0 {
		r.Detail = "ffprobe found no audio stream to decode"
		return r
	}
	// One 16-bit channel at the decoder's own rate: two bytes a sample.
	counter := &counting{}
	var output io.Writer = counter
	decodeArgs := []string{"-nostdin", "-hide_banner", "-v", "error"}
	if capture != nil {
		output = capture
		decodeArgs = append(decodeArgs, "-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1")
	}
	decodeArgs = append(decodeArgs, inputOptions...)
	decodeArgs = append(decodeArgs, "-i", input, "-map", "0:a:0", "-ac", "1", "-ar", strconv.Itoa(r.SampleRate), "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1")
	stderr, code, err := runCommand(ctx, ffmpeg, output, decodeArgs...)
	if capture != nil {
		counter.n = capture.n
	}
	if ctx.Err() != nil {
		r.Detail = "ffmpeg did not finish within the deadline"
		return r
	}
	r.DecodedSamples = counter.n / 2
	r.DecodedDurationMs = r.DecodedSamples * 1000 / int64(r.SampleRate)
	for _, line := range strings.Split(stderr, "\n") {
		if line = textField(line, 300); line == "" {
			continue
		}
		// FFmpeg folds repeats into one line that counts them.
		if rest, ok := strings.CutPrefix(line, "Last message repeated "); ok {
			if n, err := strconv.Atoi(strings.TrimSuffix(rest, " times")); err == nil && n > 0 {
				r.Errors += min(n, 1_000_000)
				continue
			}
		}
		r.Errors++
		if len(r.Messages) < maxMessages {
			r.Messages = append(r.Messages, line)
		} else {
			r.MessagesOmitted++
		}
	}
	switch {
	case counter.n%2 != 0:
		r.Detail = "ffmpeg wrote a partial sample"
	case code < 0 && err != nil:
		r.Detail = "ffmpeg could not run: " + textField(err.Error(), 200)
	case r.DecodedSamples == 0:
		r.Detail = "ffmpeg decoded no audio"
		if r.Errors > 0 {
			r.Detail += ": " + r.Messages[0]
		}
	case code != 0 || r.Errors > 0:
		r.Status = "errors_reported"
	default:
		r.Status = "passed"
	}
	return r
}

type counting struct{ n int64 }

func (c *counting) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// run executes exe with args, streams stdout to out, and returns at most
// 64 KiB of stderr, or of stdout when out is io.Discard, with the exit code.
func run(ctx context.Context, exe string, out io.Writer, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.WaitDelay = 2 * time.Second
	text := &capped{}
	if out == io.Discard {
		cmd.Stdout, cmd.Stderr = text, io.Discard
	} else {
		cmd.Stdout, cmd.Stderr = out, text
	}
	err := cmd.Run()
	code := -1
	var exit *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exit):
		code = exit.ExitCode()
	}
	return text.String(), code, err
}

type capped struct{ b bytes.Buffer }

func (c *capped) Write(p []byte) (int, error) {
	if room := maxOutput - c.b.Len(); room > 0 {
		c.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (c *capped) String() string { return c.b.String() }

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// textField keeps tool text printable, single-line, and bounded.
func textField(s string, limit int) string {
	s = strings.Join(strings.FieldsFunc(strings.ToValidUTF8(s, ""), func(r rune) bool { return r < 0x20 || r == 0x7f }), " ")
	s = strings.TrimSpace(s)
	if len(s) > limit {
		end := limit
		for end > 0 && !utf8.RuneStart(s[end]) {
			end--
		}
		s = s[:end]
	}
	return s
}
