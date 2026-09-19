package llm_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/feytox/kabanbot/internal/llm"
)

// flaky fails with errs in order, then succeeds.
type flaky struct {
	errs  []error
	calls int
}

func (f *flaky) Complete(context.Context, llm.Request) (llm.Response, error) {
	f.calls++
	if f.calls <= len(f.errs) {
		return llm.Response{}, f.errs[f.calls-1]
	}
	return llm.Response{Text: "ok"}, nil
}

func status(code int) error { return &llm.Error{Provider: "Test", Status: code} }

var policy = llm.RetryPolicy{Attempts: 4, BaseDelay: time.Second, MaxDelay: 3 * time.Second, MaxRetryAfter: time.Minute}

func TestRetryGrowsDelaysAndSucceeds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &flaky{errs: []error{status(503), status(500), status(429)}}
		var delays []time.Duration
		ctx := llm.WithRetryObserver(t.Context(), func(e llm.RetryEvent) { delays = append(delays, e.Delay) })

		start := time.Now()
		resp, err := (&llm.Retrying{Client: f, Policy: policy}).Complete(ctx, llm.Request{})
		if err != nil || resp.Text != "ok" || f.calls != 4 {
			t.Fatalf("resp = %+v, err = %v, calls = %d", resp, err, f.calls)
		}
		// 1s, 2s, then capped at 3s, each with up to 25% jitter.
		for i, base := range []time.Duration{time.Second, 2 * time.Second, 3 * time.Second} {
			if d := delays[i]; d < base || d > base*5/4 {
				t.Errorf("delay %d = %v, want %v..%v", i, d, base, base*5/4)
			}
		}
		if took := time.Since(start); took < 6*time.Second {
			t.Errorf("finished after %v, before the delays passed", took)
		}
	})
}

func TestRetryGivesUpAfterAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &flaky{errs: []error{status(503), status(503), status(503), status(503), status(503)}}
		_, err := (&llm.Retrying{Client: f, Policy: policy}).Complete(t.Context(), llm.Request{})
		perr, ok := errors.AsType[*llm.Error](err)
		if !ok || perr.Attempts != 4 || f.calls != 4 {
			t.Fatalf("err = %v, attempts = %d, calls = %d", err, perr.Attempts, f.calls)
		}
	})
}

func TestRetryHonorsRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := &flaky{errs: []error{&llm.Error{Status: 429, RetryAfter: 40 * time.Second}}}
		start := time.Now()
		if _, err := (&llm.Retrying{Client: f, Policy: policy}).Complete(t.Context(), llm.Request{}); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took < 40*time.Second {
			t.Errorf("retried after %v, want at least the 40s the provider asked for", took)
		}
	})
}

func TestRetrySkipsHopelessCases(t *testing.T) {
	for name, errs := range map[string][]error{
		"client error":     {status(400)},
		"bad key":          {status(401)},
		"not an llm.Error": {errors.New("boom")},
		"daily quota":      {&llm.Error{Status: 429, RetryAfter: 10 * time.Hour}},
		"unreachable host": {&llm.Error{Err: errors.New("no such host")}},
	} {
		synctest.Test(t, func(t *testing.T) {
			f := &flaky{errs: errs}
			if _, err := (&llm.Retrying{Client: f, Policy: policy}).Complete(t.Context(), llm.Request{}); err == nil || f.calls != 1 {
				t.Errorf("%s: err = %v, calls = %d; want one failed call", name, err, f.calls)
			}
		})
	}
}

func TestRetryStopsBeforeDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		f := &flaky{errs: []error{&llm.Error{Status: 429, RetryAfter: 45 * time.Second}}}
		_, err := (&llm.Retrying{Client: f, Policy: policy}).Complete(ctx, llm.Request{})
		if perr, ok := errors.AsType[*llm.Error](err); !ok || perr.Status != 429 || f.calls != 1 {
			t.Fatalf("err = %v, calls = %d; want the provider's error at once", err, f.calls)
		}
	})
}
