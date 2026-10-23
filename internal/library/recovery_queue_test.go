package library

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func queueFixture(n int) (*recoverySchedule, []recoveryCase) {
	s := &recoverySchedule{jobs: map[recoveryCase]*scheduledRecoveryCase{}}
	var cases []recoveryCase
	for i := range n {
		key := recoveryCase{"queue", strconv.Itoa(i)}
		cases = append(cases, key)
		s.jobs[key] = &scheduledRecoveryCase{done: make(chan recoveryCaseResult, 1)}
	}
	return s, cases
}

func TestRecoveryQueueReservesOrdinaryLaneAndConsumesAllSelections(t *testing.T) {
	s, cases := queueFixture(5)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer s.wait.Wait()
	defer cancel()
	slots := make(chan struct{}, 3)
	slots <- struct{}{}
	starts := make(chan recoveryCase, 5)
	releases := make(chan struct{}, 5)
	var active, maximum atomic.Int32
	s.startWorkers(ctx, cases, slots, func(key recoveryCase) recoveryCaseResult {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); old < n; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		starts <- key
		select {
		case <-releases:
		case <-ctx.Done():
			return recoveryCaseResult{err: ctx.Err()}
		}
		return recoveryCaseResult{output: []byte(key.selected)}
	})
	next := func() recoveryCase {
		t.Helper()
		select {
		case key := <-starts:
			return key
		case <-ctx.Done():
			t.Fatal("queue failed to dispatch", ctx.Err())
			return recoveryCase{}
		}
	}
	first, second := next(), next()
	if first == second || first != cases[0] && first != cases[1] || second != cases[0] && second != cases[1] || active.Load() != 2 {
		t.Fatal("reserved lane or first dispatch", first, second, active.Load())
	}
	select {
	case key := <-starts:
		t.Fatal("reserved ordinary lane used", key)
	default:
	}
	releases <- struct{}{}
	if key := next(); key != cases[2] {
		t.Fatal("queued item was stranded behind a reserved worker", key)
	}
	<-slots // Ordinary serial work finishes; the third lane is now available.
	if key := next(); key != cases[3] || active.Load() != 3 {
		t.Fatal("third lane failed to take the next item", key, active.Load())
	}
	for range 4 {
		releases <- struct{}{}
	}
	done := make(chan struct{})
	go func() { s.wait.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("queue did not finish", ctx.Err())
	}
	if maximum.Load() != 3 || active.Load() != 0 || len(slots) != 0 {
		t.Fatal("worker/token leak or concurrency changed", maximum.Load(), active.Load(), len(slots))
	}
	if key := next(); key != cases[4] {
		t.Fatal("last selection lost", key)
	}
	for _, key := range cases {
		select {
		case result := <-s.jobs[key].done:
			if result.err != nil || string(result.output) != key.selected {
				t.Fatal(key, result)
			}
		default:
			t.Fatal("selection did not publish its own result", key)
		}
	}
}

func TestRecoveryQueueCancellationCompletesUnstartedSelections(t *testing.T) {
	for _, beforeStart := range []bool{true, false} {
		t.Run(strconv.FormatBool(beforeStart), func(t *testing.T) {
			s, cases := queueFixture(7)
			ctx, cancel := context.WithCancel(t.Context())
			defer s.wait.Wait()
			defer cancel()
			slots := make(chan struct{}, 3)
			slots <- struct{}{}
			started := make(chan struct{}, 7)
			var calls atomic.Int32
			if beforeStart {
				cancel()
			}
			s.startWorkers(ctx, cases, slots, func(recoveryCase) recoveryCaseResult {
				calls.Add(1)
				started <- struct{}{}
				<-ctx.Done()
				return recoveryCaseResult{err: ctx.Err()}
			})
			if !beforeStart {
				for range 2 {
					select {
					case <-started:
					case <-time.After(5 * time.Second):
						t.Fatal("active queue did not start")
					}
				}
				cancel()
			}
			done := make(chan struct{})
			go func() { s.wait.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("canceled collectors would hang")
			}
			want := int32(2)
			if beforeStart {
				want = 0
			}
			if calls.Load() != want || len(slots) != 1 {
				t.Fatal("canceled work executed or lost reserved lane", calls.Load(), len(slots))
			}
			for _, key := range cases {
				select {
				case result := <-s.jobs[key].done:
					if !errors.Is(result.err, context.Canceled) || result.output != nil {
						t.Fatal("canceled selection became success", key, result)
					}
				default:
					t.Fatal("unstarted selection has no cancellation result", key)
				}
			}
		})
	}
}
