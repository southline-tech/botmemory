package drive

import (
	"context"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// Retry policy for transient Drive failures (HTTP 429, 5xx, transport
// errors): bounded exponential backoff with full jitter, honoring
// Retry-After. Defaults: 5 attempts, 500ms base, 16s cap.
type Retry struct {
	MaxAttempts int
	Base, Max   time.Duration
	// Sleep is injectable for tests; returns ctx.Err() if cancelled.
	Sleep func(ctx context.Context, d time.Duration) error
	// Jitter returns a value in [0,1); injectable for tests.
	Jitter func() float64
}

func DefaultRetry() Retry {
	return Retry{MaxAttempts: 5, Base: 500 * time.Millisecond, Max: 16 * time.Second, Sleep: sleepCtx, Jitter: rand.Float64}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryable(code int) bool { return code == http.StatusTooManyRequests || code >= 500 }

// delay for attempt n (0-based): full jitter over min(Max, Base*2^n).
func (r Retry) delay(n int, resp *http.Response) time.Duration {
	if resp != nil {
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s >= 0 {
			if d := time.Duration(s) * time.Second; d <= r.Max {
				return d
			}
			return r.Max
		}
	}
	ceil := r.Base << n
	if ceil > r.Max || ceil <= 0 {
		ceil = r.Max
	}
	return time.Duration(r.Jitter() * float64(ceil))
}

// send performs req with retries. Request bodies are rewound via GetBody
// (set automatically by http.NewRequest for bytes readers). Retried creates
// (POST) may leave a duplicate on Drive if the first attempt actually
// succeeded; lookups resolve duplicates oldest-createdTime-first, and both
// copies hold the same bytes.
func (d *Backend) send(req *http.Request) (*http.Response, error) {
	r := d.Retry
	if r.MaxAttempts <= 0 {
		r = DefaultRetry()
	}
	ctx := req.Context()
	for n := 0; ; n++ {
		if n > 0 && req.GetBody != nil {
			b, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			req.Body = b
		}
		resp, err := d.HC.Do(req)
		last := n+1 >= r.MaxAttempts
		if err == nil && !retryable(resp.StatusCode) || last || ctx.Err() != nil {
			return resp, err
		}
		if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
			return resp, err // body not re-sendable
		}
		wait := r.delay(n, resp)
		if resp != nil {
			log.Printf("drive: %s %s: HTTP %d, retry %d/%d in %s", req.Method, req.URL.Path, resp.StatusCode, n+1, r.MaxAttempts-1, wait)
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
		} else {
			log.Printf("drive: %s %s: %v, retry %d/%d in %s", req.Method, req.URL.Path, err, n+1, r.MaxAttempts-1, wait)
		}
		if err := r.Sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}
