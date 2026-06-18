package upcloud

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud"
)

// retryPolicy bounds how transient API/network failures are retried.
type retryPolicy struct {
	maxAttempts int           // total attempts, including the first
	baseDelay   time.Duration // backoff before the second attempt
	maxDelay    time.Duration // cap on the (exponential) backoff
}

// defaultRetryPolicy is conservative: a few attempts with exponential backoff,
// enough to ride out a brief 5xx/429/network blip without masking a real fault.
func defaultRetryPolicy() retryPolicy {
	return retryPolicy{maxAttempts: 4, baseDelay: 500 * time.Millisecond, maxDelay: 8 * time.Second}
}

// isRetryable reports whether err is a TRANSIENT failure worth retrying:
// an UpCloud API error (*upcloud.Problem) with HTTP 429 or 5xx, or a network
// timeout. Context cancellation/deadline and client errors (4xx other than 429)
// are NOT retryable — retrying them only wastes time or repeats a rejected call.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var prob *upcloud.Problem
	if errors.As(err, &prob) {
		return prob.Status == http.StatusTooManyRequests || (prob.Status >= 500 && prob.Status <= 599)
	}
	var nerr net.Error
	if errors.As(err, &nerr) {
		return nerr.Timeout()
	}
	return false
}

// withRetry runs fn, retrying transient failures (isRetryable) with bounded
// exponential backoff. It honors ctx: it never sleeps or retries past
// cancellation or the deadline, and returns the last underlying error (not a
// bare context error) when a retry is cut short, so callers see why it failed.
func withRetry[T any](ctx context.Context, p retryPolicy, fn func() (T, error)) (T, error) {
	var zero T
	delay := p.baseDelay
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		v, err := fn()
		if err == nil {
			return v, nil
		}
		if attempt >= p.maxAttempts || !isRetryable(err) {
			return zero, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, err
		case <-timer.C:
		}
		if delay <= p.maxDelay/2 {
			delay *= 2
		} else {
			delay = p.maxDelay
		}
	}
}

// retryErr is the error-only convenience over withRetry for operations that
// return no value (Stop, Delete).
func retryErr(ctx context.Context, p retryPolicy, fn func() error) error {
	_, err := withRetry(ctx, p, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}
