package discovery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/blisspixel/nemalo/internal/transfer"
)

// requestPacer shares quota and dispatch ordering across all calls of a provider.
// Client initialization also uses mu; callers must not copy this value.
type requestPacer struct {
	mu          sync.Mutex
	next, until time.Time
	dispatch    chan struct{}
}

// afterResponse publishes the local interval and any source quota together.
func (a *requestPacer) afterResponse(resp *http.Response, pace time.Duration) time.Time {
	now := time.Now()
	var retryAt time.Time
	if resp != nil && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) {
		retryAt = retryUntil(resp.Header.Get("Retry-After"), now)
	}
	a.mu.Lock()
	a.next = maxTime(maxTime(a.next, now.Add(pace)), retryAt)
	a.until = maxTime(a.until, retryAt)
	a.mu.Unlock()
	return retryAt
}

func (a *requestPacer) do(client *http.Client, req *http.Request, pace time.Duration) (*http.Response, time.Time, error) {
	a.mu.Lock()
	if a.dispatch == nil {
		a.dispatch = make(chan struct{}, 1)
	}
	gate := a.dispatch
	a.mu.Unlock()
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-req.Context().Done():
		return nil, time.Time{}, req.Context().Err()
	}
	if err := a.wait(req.Context(), pace); err != nil {
		return nil, time.Time{}, err
	}
	resp, err := client.Do(req)
	// Dispatch or header delivery can be delayed after the grant. Starting
	// the next interval here prevents that delay from compressing requests.
	// Publish a source quota before releasing the dispatch gate, too.
	retryAt := a.afterResponse(resp, pace)
	return resp, retryAt, err
}

// wait spaces every provider request by pace.
func (a *requestPacer) wait(ctx context.Context, pace time.Duration) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.mu.Lock()
		now := time.Now()
		if a.until.Sub(now) > maxRetryWait {
			until := a.until
			a.mu.Unlock()
			return fmt.Errorf("provider asked not to be asked again until %s", until.UTC().Format(time.RFC3339))
		}
		if !now.Before(a.next) {
			a.next = now.Add(pace)
			a.mu.Unlock()
			return nil
		}
		delay := a.next.Sub(now)
		a.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// Recheck the deadline: another caller may take the slot, or
			// Retry-After may extend it while this timer waits.
		}
	}
}

// get sends one request at provider's pace. A transport failure or a 429 or
// 5xx answer before any body is tried again, at most three times in all,
// each at the same pace and after any Retry-After of at most 30 seconds.
func (a *requestPacer) get(ctx context.Context, client *http.Client, pace time.Duration, method, raw string) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "nemalo/0.1 (+https://github.com/blisspixel/nemalo)")
		req.Header.Set("Accept-Encoding", "identity")
		resp, retryAt, err := a.do(client, req, pace)
		retry := false
		switch {
		case err != nil:
			retry = transfer.Transient(err) && ctx.Err() == nil
		case resp.StatusCode == 429 || resp.StatusCode >= 500:
			// The requested wait holds for every later request too, and a
			// wait longer than a retry allows stops them until it passes.
			retry = time.Until(retryAt) <= maxRetryWait
		}
		if !retry || attempt >= downloadAttempts {
			return resp, err
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
		}
	}
}
