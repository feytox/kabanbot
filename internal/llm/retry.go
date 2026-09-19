package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

// Error is a failed call to a provider, described well enough to decide on a retry
// and to tell the user what went wrong.
type Error struct {
	// Provider names the API, e.g. "Gemini".
	Provider string
	// Status is the HTTP status. Zero means the provider could not be reached.
	Status int
	// Message is the provider's own explanation, if any.
	Message string
	// RetryAfter is how long the provider asked to wait before retrying. Zero means it did not say.
	RetryAfter time.Duration
	// Attempts is how many times the call was made before giving up.
	Attempts int
	Err      error
}

func (e *Error) Error() string {
	if e.Status == 0 {
		return fmt.Sprintf("%s: request failed: %v", e.Provider, e.Err)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Provider, e.Status, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// Temporary reports whether the same request may succeed later: rate limits, server errors,
// timeouts and dropped connections. Other network errors, such as an unknown host or
// an address blocked by netguard, will fail again.
func (e *Error) Temporary() bool {
	switch e.Status {
	case 0:
		if ne, ok := errors.AsType[net.Error](e.Err); ok && ne.Timeout() {
			return true
		}
		return errors.Is(e.Err, syscall.ECONNRESET) || errors.Is(e.Err, syscall.ECONNREFUSED) ||
			errors.Is(e.Err, io.ErrUnexpectedEOF)
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return e.Status >= 500
}

// RetryPolicy says how often and how long to retry temporary errors.
type RetryPolicy struct {
	// Attempts is the total number of calls, including the first one.
	Attempts int
	// BaseDelay is the wait before the first retry. Each next wait doubles, up to MaxDelay.
	BaseDelay time.Duration
	MaxDelay  time.Duration
	// MaxRetryAfter is the longest wait a provider may ask for. A longer one,
	// e.g. an exhausted daily quota, is not worth waiting for.
	MaxRetryAfter time.Duration
}

// DefaultRetryPolicy suits free tiers that allow a few requests per minute.
var DefaultRetryPolicy = RetryPolicy{
	Attempts:      5,
	BaseDelay:     2 * time.Second,
	MaxDelay:      30 * time.Second,
	MaxRetryAfter: time.Minute,
}

// RetryEvent describes a retry that is about to happen.
type RetryEvent struct {
	// Attempt is the number of the failed call, starting at 1.
	Attempt  int
	Attempts int
	Delay    time.Duration
	Err      *Error
}

type observerKey struct{}

// WithRetryObserver returns a context whose calls report retries to f, e.g. to show progress.
func WithRetryObserver(ctx context.Context, f func(RetryEvent)) context.Context {
	return context.WithValue(ctx, observerKey{}, f)
}

// Retrying wraps a Client and retries its temporary errors with growing delays.
type Retrying struct {
	Client Client
	Policy RetryPolicy
}

// WithRetry wraps c with DefaultRetryPolicy.
func WithRetry(c Client) *Retrying { return &Retrying{Client: c, Policy: DefaultRetryPolicy} }

// Complete implements Client.
func (r *Retrying) Complete(ctx context.Context, req Request) (Response, error) {
	p := r.Policy
	for attempt := 1; ; attempt++ {
		resp, err := r.Client.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		perr, ok := errors.AsType[*Error](err)
		if !ok || !perr.Temporary() || ctx.Err() != nil {
			return Response{}, err
		}
		perr.Attempts = attempt
		if attempt >= p.Attempts {
			return Response{}, err
		}

		delay, ok := p.delay(attempt, perr.RetryAfter)
		if !ok {
			return Response{}, err
		}
		if deadline, has := ctx.Deadline(); has && time.Until(deadline) < delay {
			// Waiting would only end in a timeout; report the provider's error instead.
			return Response{}, err
		}
		if f, _ := ctx.Value(observerKey{}).(func(RetryEvent)); f != nil {
			f(RetryEvent{Attempt: attempt, Attempts: p.Attempts, Delay: delay, Err: perr})
		}

		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return Response{}, err
		case <-t.C:
		}
	}
}

// delay returns the wait after the given failed attempt. It reports false when
// the provider asked to wait longer than MaxRetryAfter.
func (p RetryPolicy) delay(attempt int, retryAfter time.Duration) (time.Duration, bool) {
	if retryAfter > p.MaxRetryAfter {
		return 0, false
	}
	d := min(p.BaseDelay<<min(attempt-1, 16), p.MaxDelay)
	// Jitter keeps several chats from retrying in lockstep.
	d += time.Duration(rand.Int64N(int64(d)/4 + 1))
	return max(d, retryAfter), true
}

// ParseRetryAfter reads the Retry-After header (seconds or an HTTP date) and its
// millisecond variant used by some OpenAI-compatible servers. Zero means none.
func ParseRetryAfter(h http.Header) time.Duration {
	if ms, err := strconv.ParseFloat(h.Get("Retry-After-Ms"), 64); err == nil && ms > 0 {
		return time.Duration(ms * float64(time.Millisecond))
	}
	v := h.Get("Retry-After")
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return 0
}
