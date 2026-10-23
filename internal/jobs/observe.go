package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/safeio"
)

const (
	MaxObservedEntries      = 4096
	MaxObservedJournalBytes = 6 << 20
	MaxObservedReadBytes    = 32 << 20
	MaxObservedRecords      = maxProgress + 4
)

// JournalJob is internal journal evidence for application relationship matching.
// It contains private request/result data and is not a public snapshot DTO.
// Raw JSON preserves exact integers rather than going through float64.
type JournalJob struct {
	ID, Kind, Status, Created, Updated string
	Request, Context, Result           json.RawMessage `json:"-"`
	Progress                           *Progress
	Error, Detail                      string `json:"-"`
}

// fold is the one record fold used by legacy writer reads and passive reads.
// Stricter passive validation runs before it, leaving legacy replay unchanged.
func (j *JournalJob) fold(r record) {
	j.Updated = r.At
	switch r.Event {
	case Queued:
		j.Kind, j.Request, j.Context, j.Created, j.Status = r.Kind, r.Request, r.Context, r.At, Queued
	case Running:
		if !final(j.Status) {
			j.Status = Running
		}
	case "progress":
		j.Progress = r.Progress
	case "finished":
		if !final(j.Status) {
			j.Status, j.Result, j.Error, j.Detail = r.Status, r.Result, r.Error, r.Detail
		}
	}
}

// Observation reports work and coarse coverage only. File names, raw failures,
// requests and results are deliberately absent from this public-safe report.
type Observation struct {
	Store     string   `json:"store"`
	Entries   int      `json:"entries_examined"`
	Journals  int      `json:"journals_read"`
	ReadBytes int      `json:"journal_bytes_read"`
	Complete  bool     `json:"complete"`
	Problems  []string `json:"problems"`
}

func (o *Observation) problem(code string) {
	o.Complete = false
	for _, p := range o.Problems {
		if p == code {
			return
		}
	}
	o.Problems = append(o.Problems, code)
}

// Observe reads existing capabilities only. It never opens a Store, job locks,
// cancellation markers or write handles. A torn committed prefix is visited as
// incomplete. Any returned error invalidates all previously visited records.
// The callback must not retain the raw journal evidence as public output.
func Observe(ctx context.Context, directory string, visit func(JournalJob, string) error) (out Observation, err error) {
	out = Observation{Store: "observed", Complete: true, Problems: []string{}}
	if !filepath.IsAbs(directory) || visit == nil {
		return out, errors.New("job observation requires an absolute folder and callback")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	root, err := safeio.OpenRoot(directory)
	if errors.Is(err, os.ErrNotExist) {
		out.Store = "missing"
		return out, nil
	}
	if err != nil {
		return out, err
	}
	defer func() {
		opened, statErr := root.Stat(".")
		named, namedErr := safeio.Lstat(directory)
		if statErr != nil || namedErr != nil || !named.IsDir() || named.Mode()&os.ModeSymlink != 0 || !os.SameFile(named, opened) {
			err = errors.Join(err, errors.New("job directory changed during observation"), statErr, namedErr)
		}
		err = errors.Join(err, ctx.Err(), root.Close())
	}()
	dir, err := root.Open(".")
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		// Do not spend an uncounted extra directory read to prove EOF. At
		// exactly the ceiling, completeness is conservatively unavailable.
		if out.Entries == MaxObservedEntries {
			out.problem("directory_limit")
			break
		}
		entries, readErr := dir.ReadDir(min(64, MaxObservedEntries-out.Entries))
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			out.problem("directory_read_incomplete")
			break
		}
		for _, entry := range entries {
			out.Entries++
			name, ok := strings.CutSuffix(entry.Name(), ".jsonl")
			if !ok || !validID("job:"+name) {
				continue
			}
			if out.ReadBytes >= MaxObservedReadBytes {
				out.problem("journal_read_limit")
				return out, nil
			}
			out.Journals++
			j, integrity, used, fileErr := observeJournal(ctx, root, entry.Name(), MaxObservedReadBytes-out.ReadBytes)
			out.ReadBytes += used
			if err := ctx.Err(); err != nil {
				return out, err
			}
			if fileErr != nil {
				out.problem("journal_unavailable_or_invalid")
				continue
			}
			if integrity != "complete" {
				out.problem("journal_incomplete")
			}
			if err := visit(j, integrity); err != nil {
				return out, err
			}
		}
		if errors.Is(readErr, io.EOF) || len(entries) == 0 {
			break
		}
	}
	return out, nil
}

func observeJournal(ctx context.Context, root *os.Root, name string, allowance int) (job JournalJob, integrity string, used int, err error) {
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() == 0 || before.Size() > MaxObservedJournalBytes {
		return job, "", 0, errors.New("invalid journal entry")
	}
	f, err := safeio.OpenRegular(root, name, before, os.O_RDONLY)
	if err != nil {
		return job, "", 0, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	// Include reads from failed or growing files in the shared allowance.
	data, err := io.ReadAll(io.LimitReader(observationReader{ctx, f}, int64(min(allowance, MaxObservedJournalBytes))))
	used = len(data)
	if err != nil || used > MaxObservedJournalBytes || int64(used) != before.Size() {
		return job, "", used, errors.New("incomplete journal read")
	}
	if err := ctx.Err(); err != nil {
		return job, "", used, err
	}
	after, err := f.Stat()
	if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return job, "", used, errors.New("journal changed during read")
	}
	named, err := root.Lstat(name)
	if err != nil || !named.Mode().IsRegular() || !os.SameFile(named, after) {
		return job, "", used, errors.New("journal replaced during read")
	}
	return parseObservedJournal(ctx, "job:"+strings.TrimSuffix(name, ".jsonl"), data, used)
}

type observationReader struct {
	ctx context.Context
	r   io.Reader
}

func (r observationReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func parseObservedJournal(ctx context.Context, id string, data []byte, used int) (JournalJob, string, int, error) {
	j := JournalJob{ID: id}
	integrity := "complete"
	if len(data) == 0 || !utf8.Valid(data) {
		return j, "", used, errors.New("empty or non-UTF-8 journal")
	}
	if data[len(data)-1] != '\n' {
		integrity = "torn_tail"
		data = data[:bytes.LastIndexByte(data, '\n')+1]
	}
	if bytes.Count(data, []byte{'\n'}) > MaxObservedRecords {
		return j, "", used, errors.New("journal record limit")
	}
	lines := bytes.Split(data, []byte{'\n'})
	progress := 0
	for i, line := range lines[:len(lines)-1] {
		if err := ctx.Err(); err != nil {
			return j, "", used, err
		}
		var r record
		if json.Unmarshal(line, &r) != nil || !validObservedRecord(j, r, line, i) {
			return JournalJob{}, "", used, errors.New("unsupported or invalid journal record")
		}
		if r.Event == "progress" {
			progress++
			if progress > maxProgress {
				return JournalJob{}, "", used, errors.New("journal progress limit")
			}
		}
		j.fold(r)
	}
	if j.Created == "" {
		return j, "", used, errors.New("no committed start")
	}
	return j, integrity, used, nil
}

func validObservedRecord(j JournalJob, r record, line []byte, index int) bool {
	at, err := time.Parse(time.RFC3339Nano, r.At)
	if err != nil || at.IsZero() || at.UTC().Format(time.RFC3339Nano) != r.At || r.SchemaVersion != 1 {
		return false
	}
	canonical, err := json.Marshal(r)
	if err != nil || !bytes.Equal(canonical, line) {
		return false
	}
	for _, raw := range []json.RawMessage{r.Request, r.Context, r.Result} {
		if len(raw) > 0 && !uniqueRecordedKeys(raw) {
			return false
		}
	}
	expected := record{SchemaVersion: 1, At: r.At, Event: r.Event}
	switch r.Event {
	case Queued:
		if index != 0 || r.Kind == "" || len(r.Kind) > 64 || len(r.Request) == 0 || len(r.Request) > maxRequest || len(r.Context) > maxContext {
			return false
		}
		expected.Kind, expected.Request, expected.Context = r.Kind, r.Request, r.Context
	case Running:
		if j.Status != Queued {
			return false
		}
	case "progress":
		if j.Status != Running || r.Progress == nil || r.Progress.Phase != bound(r.Progress.Phase, 64) || r.Progress.Unit != bound(r.Progress.Unit, 32) || r.Progress.Done < 0 || r.Progress.Total < 0 {
			return false
		}
		expected.Progress = r.Progress
	case "finished":
		if j.Created == "" || final(j.Status) || !final(r.Status) || len(r.Result) > maxResult || r.Error != bound(r.Error, 2000) || len(r.Detail) > 2000 {
			return false
		}
		expected.Status, expected.Result, expected.Error, expected.Detail = r.Status, r.Result, r.Error, r.Detail
	case "cancel_requested":
		// Cancellation can race with the terminal append. A late request does
		// not undo the recorded terminal outcome.
		if j.Created == "" {
			return false
		}
	default:
		return false
	}
	want, err := json.Marshal(expected)
	return err == nil && bytes.Equal(want, line)
}

// Raw subdocuments must not give two interpretations to an association. Keep
// numbers exact, reject duplicate object keys, and bound parser nesting.
func uniqueRecordedKeys(raw []byte) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 64 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, composite := token.(json.Delim)
		if !composite {
			return true
		}
		keys := map[string]bool{}
		for d.More() {
			if delim == '{' {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || keys[name] {
					return false
				}
				keys[name] = true
			}
			if !value(depth + 1) {
				return false
			}
		}
		end, err := d.Token()
		return err == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
	}
	if !value(0) {
		return false
	}
	_, err := d.Token()
	return errors.Is(err, io.EOF)
}
