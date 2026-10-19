// Package jobs runs long operations as durable jobs: each has an ID and an
// append-only journal in Nemalo's state folder, so its status, progress, and
// result outlive the connection or process that started it. A job runs in the
// process that started it, which holds an operating system lock on it for its
// whole run; any process that finds a job unfinished and its lock free
// records it as interrupted. There is no queue service or daemon.
package jobs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/blisspixel/nemalo/internal/safeio"
)

// Job states. Queued and running jobs are unfinished; the others are final.
const (
	Queued      = "queued"
	Running     = "running"
	Succeeded   = "succeeded"
	Failed      = "failed"
	Cancelled   = "cancelled"
	Interrupted = "interrupted"
)

// Progress is the last reported step of a running job: a phase such as
// "transfer" and, when known, how much of a total is done.
type Progress struct {
	Phase string `json:"phase"`
	Done  int64  `json:"done,omitempty"`
	Total int64  `json:"total,omitempty"`
	Unit  string `json:"unit,omitempty"`
}

// Job is a job's state as its journal records it.
type Job struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	Status   string    `json:"status"`
	Created  string    `json:"created"`
	Updated  string    `json:"updated"`
	Request  any       `json:"request"`
	Progress *Progress `json:"progress,omitempty"`
	// Result is the operation's own result when it returned one, success or
	// not; Error is its error.
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	// Owner tells whether this process runs the job. An unfinished job run
	// elsewhere can be read here but cancelled only there.
	Owner string `json:"owner"`
	// Detail explains an interrupted or refused state.
	Detail string `json:"detail,omitempty"`
}

type record struct {
	SchemaVersion int             `json:"schema_version"`
	Event         string          `json:"event"`
	At            string          `json:"at"`
	Kind          string          `json:"kind,omitempty"`
	Request       json.RawMessage `json:"request,omitempty"`
	Progress      *Progress       `json:"progress,omitempty"`
	Status        string          `json:"status,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         string          `json:"error,omitempty"`
	Detail        string          `json:"detail,omitempty"`
}

const (
	maxRequest   = 64 << 10
	maxResult    = 1 << 20
	maxJournal   = 4 << 20
	maxProgress  = 1000
	progressGap  = time.Second
	keepFinished = 30 * 24 * time.Hour
	maxListed    = 100
)

// ErrBusy means the store already holds as many running and queued jobs as
// it allows.
var ErrBusy = errors.New("too many jobs are running or queued; wait for one to finish")

// ErrNotFound means no job has that ID.
var ErrNotFound = errors.New("no job has that ID")

// Run is one job's work. It reports progress through report, which is cheap
// to call often, and returns a result to record, success or not.
type Run func(ctx context.Context, report func(Progress)) (any, error)

type running struct {
	cancel context.CancelFunc
	lock   *os.File
	done   chan struct{}
}

// Store runs jobs and reads their journals in one folder.
type Store struct {
	dir        string
	slots      chan struct{}
	maxPending int
	mu         sync.Mutex
	jobs       map[string]*running
	closed     bool
	wg         sync.WaitGroup
}

// Open opens or creates the jobs folder, records as interrupted every
// unfinished job whose owner is gone, and removes finished jobs older than
// 30 days. At most maxRunning jobs run at once and maxPending wait.
func Open(dir string, maxRunning, maxPending int) (*Store, error) {
	if !filepath.IsAbs(dir) || maxRunning < 1 || maxPending < 0 {
		return nil, errors.New("jobs need an absolute folder and at least one running slot")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, slots: make(chan struct{}, maxRunning), maxPending: maxRunning + maxPending, jobs: map[string]*running{}}
	ids, err := s.ids()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		j, err := s.Get(id)
		if err != nil {
			continue
		}
		if final(j.Status) {
			if updated, perr := time.Parse(time.RFC3339Nano, j.Updated); perr == nil && time.Since(updated) > keepFinished {
				_ = os.Remove(s.journal(fileID(id)))
				_ = os.Remove(s.lockName(fileID(id)))
			}
		}
	}
	return s, nil
}

func final(status string) bool {
	return status == Succeeded || status == Failed || status == Cancelled || status == Interrupted
}

func (s *Store) journal(id string) string  { return filepath.Join(s.dir, id+".jsonl") }
func (s *Store) lockName(id string) string { return filepath.Join(s.dir, id+".lock") }

func validID(id string) bool {
	rest, ok := strings.CutPrefix(id, "job:")
	if !ok || len(rest) != 32 {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil && strings.ToLower(rest) == rest
}

func fileID(id string) string { return strings.TrimPrefix(id, "job:") }

// Start records a queued job and runs it when a slot is free. The request is
// recorded as given, so it should hold only what the job needs.
func (s *Store) Start(kind string, request any, run Run) (Job, error) {
	req, err := json.Marshal(request)
	if err != nil || len(req) > maxRequest {
		return Job{}, errors.New("a job request must be JSON of at most 64 KiB")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Job{}, errors.New("this server is closing and starts no new jobs")
	}
	if len(s.jobs) >= s.maxPending {
		s.mu.Unlock()
		return Job{}, ErrBusy
	}
	id, lock, err := s.create(kind, req)
	if err != nil {
		s.mu.Unlock()
		return Job{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{cancel: cancel, lock: lock, done: make(chan struct{})}
	s.jobs[id] = r
	s.wg.Add(1)
	s.mu.Unlock()
	go s.run(ctx, id, r, run)
	return s.Get(id)
}

// create makes a job's lock, held from here to the end of its run, and its
// first journal line.
func (s *Store) create(kind string, req []byte) (string, *os.File, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	id := "job:" + hex.EncodeToString(raw)
	lock, err := os.OpenFile(s.lockName(fileID(id)), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", nil, err
	}
	if err := safeio.TryLock(lock, true); err != nil {
		_ = lock.Close()
		return "", nil, err
	}
	if err := s.append(id, record{Event: Queued, Kind: kind, Request: req}, true); err != nil {
		_ = safeio.Unlock(lock)
		_ = lock.Close()
		return "", nil, err
	}
	return id, lock, nil
}

func (s *Store) run(ctx context.Context, id string, r *running, run Run) {
	defer s.wg.Done()
	defer close(r.done)
	defer func() {
		s.mu.Lock()
		delete(s.jobs, id)
		s.mu.Unlock()
		_ = safeio.Unlock(r.lock)
		_ = r.lock.Close()
	}()
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		_ = s.append(id, record{Event: "finished", Status: Cancelled, Detail: "cancelled before it started"}, true)
		return
	}
	defer func() { <-s.slots }()
	_ = s.append(id, record{Event: Running}, false)
	var last time.Time
	reports := 0
	report := func(p Progress) {
		// Progress is journaled at most once a second and a thousand times.
		if time.Since(last) < progressGap || reports >= maxProgress {
			return
		}
		last, reports = time.Now(), reports+1
		p.Phase, p.Unit = bound(p.Phase, 64), bound(p.Unit, 32)
		_ = s.append(id, record{Event: "progress", Progress: &p}, false)
	}
	var result any
	var err error
	func() {
		// A panic in the work is a failed job, not a dead server.
		defer func() {
			if v := recover(); v != nil {
				err = fmt.Errorf("the job stopped unexpectedly: %v", v)
			}
		}()
		result, err = run(ctx, report)
	}()
	rec := record{Event: "finished", Status: Succeeded}
	if result != nil {
		if b, merr := json.Marshal(result); merr == nil && len(b) <= maxResult {
			rec.Result = b
		} else {
			rec.Detail = "the result was too large to record"
		}
	}
	switch {
	case ctx.Err() != nil:
		rec.Status, rec.Error = Cancelled, "cancelled"
	case err != nil:
		rec.Status, rec.Error = Failed, bound(err.Error(), 2000)
	}
	_ = s.append(id, rec, true)
}

// Cancel asks a job this process runs to stop. Its work stops at its next
// cancellation check; the job then records cancelled.
func (s *Store) Cancel(id string) (Job, error) {
	if !validID(id) {
		return Job{}, ErrNotFound
	}
	// The journal is checked first: a job is still listed as running for a
	// moment after its final line is written.
	if j, err := s.read(id); err != nil || final(j.Status) {
		if err == nil {
			err = errors.New("the job has already finished")
		}
		return j, err
	}
	s.mu.Lock()
	r := s.jobs[id]
	s.mu.Unlock()
	if r == nil {
		j, err := s.Get(id)
		if err != nil || final(j.Status) {
			return j, errors.Join(err, errors.New("the job has already finished"))
		}
		return j, errors.New("the job runs in another process, which alone can cancel it")
	}
	_ = s.append(id, record{Event: "cancel_requested"}, false)
	r.cancel()
	return s.Get(id)
}

// Wait returns once every job this process runs has finished, or ctx ends.
func (s *Store) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close starts no new jobs. Running jobs keep running; Wait outlasts them.
func (s *Store) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// Running reports how many jobs this process runs or has queued.
func (s *Store) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}

// Get reads one job. An unfinished job that no process holds is recorded as
// interrupted first, while its lock is held, so two readers cannot both
// record it.
func (s *Store) Get(id string) (Job, error) {
	if !validID(id) {
		return Job{}, ErrNotFound
	}
	j, err := s.read(id)
	if err != nil || final(j.Status) {
		return j, err
	}
	s.mu.Lock()
	_, mine := s.jobs[id]
	s.mu.Unlock()
	if mine {
		j.Owner = "this_process"
		return j, nil
	}
	lock, err := os.OpenFile(s.lockName(fileID(id)), os.O_RDWR, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.interrupt(id, j)
		}
		return j, err
	}
	defer lock.Close()
	if err := safeio.TryLock(lock, true); err != nil {
		if errors.Is(err, safeio.ErrLocked) {
			j.Owner = "another_process"
			return j, nil
		}
		return j, err
	}
	defer safeio.Unlock(lock)
	// Read again under the lock: the owner may have finished meanwhile.
	if j, err = s.read(id); err != nil || final(j.Status) {
		return j, err
	}
	return s.interrupt(id, j)
}

func (s *Store) interrupt(id string, j Job) (Job, error) {
	detail := "the process running this job ended before it finished; start it again, resuming where the operation supports it"
	if err := s.append(id, record{Event: "finished", Status: Interrupted, Detail: detail}, true); err != nil {
		return j, err
	}
	return s.read(id)
}

// List returns up to 100 jobs, newest first.
func (s *Store) List() ([]Job, error) {
	ids, err := s.ids()
	if err != nil {
		return nil, err
	}
	out := []Job{}
	for _, id := range ids {
		if j, err := s.Get(id); err == nil {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Created > out[k].Created })
	if len(out) > maxListed {
		out = out[:maxListed]
	}
	return out, nil
}

func (s *Store) ids() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if ok && e.Type().IsRegular() && validID("job:"+name) {
			ids = append(ids, "job:"+name)
		}
	}
	return ids, nil
}

var appendMu sync.Mutex

func (s *Store) append(id string, r record, sync bool) error {
	r.SchemaVersion, r.At = 1, time.Now().UTC().Format(time.RFC3339Nano)
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	appendMu.Lock()
	defer appendMu.Unlock()
	name := s.journal(fileID(id))
	if info, err := os.Lstat(name); err == nil && info.Size()+int64(len(line))+1 > maxJournal && r.Event == "progress" {
		return nil
	}
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if err := dropTornTail(f); err != nil {
		_ = f.Close()
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	var serr error
	if sync {
		serr = f.Sync()
	}
	return errors.Join(werr, serr, f.Close())
}

// dropTornTail removes an unterminated last line, as a crash mid-write
// leaves, so the next line starts on its own, and leaves f at its end.
func dropTornTail(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	if size == 0 {
		return nil
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, size-1); err != nil {
		return err
	}
	if last[0] != '\n' {
		data := make([]byte, size)
		if _, err := f.ReadAt(data, 0); err != nil {
			return err
		}
		size = int64(bytes.LastIndexByte(data, '\n') + 1)
		if err := f.Truncate(size); err != nil {
			return err
		}
	}
	_, err = f.Seek(size, io.SeekStart)
	return err
}

// read folds a journal into its job. A torn last line, as a crash leaves, is
// ignored; any other malformed line is an error.
func (s *Store) read(id string) (Job, error) {
	f, err := os.Open(s.journal(fileID(id)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxJournal+maxResult+1))
	if err != nil {
		return Job{}, err
	}
	j := Job{ID: id, Owner: "none"}
	lines := bytes.Split(data, []byte{'\n'})
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		var r record
		if err := json.Unmarshal(line, &r); err != nil || r.SchemaVersion != 1 {
			if i == len(lines)-1 {
				break
			}
			return Job{}, fmt.Errorf("job %s has an unreadable journal line", id)
		}
		j.Updated = r.At
		switch r.Event {
		case Queued:
			j.Kind, j.Request, j.Created, j.Status = r.Kind, decoded(r.Request), r.At, Queued
		case Running:
			if !final(j.Status) {
				j.Status = Running
			}
		case "progress":
			j.Progress = r.Progress
		case "finished":
			if !final(j.Status) {
				j.Status, j.Result, j.Error, j.Detail = r.Status, decoded(r.Result), r.Error, r.Detail
			}
		}
	}
	if j.Created == "" {
		return Job{}, fmt.Errorf("job %s has no start record", id)
	}
	return j, nil
}

// decoded is recorded JSON as a plain value, or nil.
func decoded(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

// bound keeps reported text single-line, valid, and at most n bytes.
func bound(s string, n int) string {
	s = strings.Join(strings.Fields(strings.ToValidUTF8(s, "")), " ")
	if len(s) > n {
		end := n
		for end > 0 && !utf8.RuneStart(s[end]) {
			end--
		}
		s = s[:end]
	}
	return s
}
