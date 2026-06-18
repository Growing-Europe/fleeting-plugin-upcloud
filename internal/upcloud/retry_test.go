package upcloud

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud"
	"github.com/UpCloudLtd/upcloud-go-api/v8/upcloud/request"
	"pgregory.net/rapid"
)

// fastPolicy keeps retry tests near-instant while preserving the attempt count.
func fastPolicy() retryPolicy {
	return retryPolicy{maxAttempts: 4, baseDelay: time.Millisecond, maxDelay: 2 * time.Millisecond}
}

// timeoutErr is a net.Error that reports a timeout.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// nonTimeoutNetErr is a net.Error that is NOT a timeout.
type nonTimeoutNetErr struct{}

func (nonTimeoutNetErr) Error() string   { return "connection refused" }
func (nonTimeoutNetErr) Timeout() bool   { return false }
func (nonTimeoutNetErr) Temporary() bool { return false }

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"problem 429", &upcloud.Problem{Status: http.StatusTooManyRequests}, true},
		{"problem 500", &upcloud.Problem{Status: 500}, true},
		{"problem 503", &upcloud.Problem{Status: 503}, true},
		{"problem 599", &upcloud.Problem{Status: 599}, true},
		{"problem 400", &upcloud.Problem{Status: 400}, false},
		{"problem 404", &upcloud.Problem{Status: 404}, false},
		{"problem 409", &upcloud.Problem{Status: 409}, false},
		{"wrapped problem 503", errors.Join(errors.New("ctx"), &upcloud.Problem{Status: 503}), true},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"net timeout", timeoutErr{}, true},
		{"net non-timeout", nonTimeoutNetErr{}, false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryable(tt.err); got != tt.want {
				t.Errorf("isRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestWithRetry_SucceedsFirstTry(t *testing.T) {
	calls := 0
	v, err := withRetry(context.Background(), fastPolicy(), func() (int, error) { calls++; return 7, nil })
	if err != nil || v != 7 || calls != 1 {
		t.Fatalf("got (v=%d, err=%v, calls=%d), want (7, nil, 1)", v, err, calls)
	}
}

func TestWithRetry_SucceedsAfterTransient(t *testing.T) {
	calls := 0
	v, err := withRetry(context.Background(), fastPolicy(), func() (int, error) {
		calls++
		if calls < 3 {
			return 0, &upcloud.Problem{Status: 503}
		}
		return 42, nil
	})
	if err != nil || v != 42 || calls != 3 {
		t.Fatalf("got (v=%d, err=%v, calls=%d), want (42, nil, 3)", v, err, calls)
	}
}

func TestWithRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	calls := 0
	_, err := withRetry(context.Background(), fastPolicy(), func() (int, error) {
		calls++
		return 0, &upcloud.Problem{Status: 500}
	})
	if calls != fastPolicy().maxAttempts {
		t.Errorf("calls = %d, want maxAttempts %d", calls, fastPolicy().maxAttempts)
	}
	var prob *upcloud.Problem
	if !errors.As(err, &prob) {
		t.Errorf("want the last *upcloud.Problem, got %v", err)
	}
}

func TestWithRetry_StopsOnNonRetryable(t *testing.T) {
	calls := 0
	_, err := withRetry(context.Background(), fastPolicy(), func() (int, error) {
		calls++
		return 0, &upcloud.Problem{Status: 400}
	})
	if calls != 1 || err == nil {
		t.Fatalf("non-retryable must be tried exactly once: calls=%d err=%v", calls, err)
	}
}

func TestWithRetry_HonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := withRetry(ctx, retryPolicy{maxAttempts: 10, baseDelay: time.Hour, maxDelay: time.Hour}, func() (int, error) {
		calls++
		cancel() // cancel during the (1-hour) backoff that follows this failure
		return 0, &upcloud.Problem{Status: 503}
	})
	// fn ran once; the backoff was cut short by cancellation; the returned error is
	// the underlying transient error, not a bare context error.
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (cancellation cuts the backoff)", calls)
	}
	var prob *upcloud.Problem
	if !errors.As(err, &prob) {
		t.Errorf("want the underlying *upcloud.Problem on cancellation, got %v", err)
	}
}

func TestWithRetry_PreCanceledContextReturnsBeforeCalling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done before the first attempt
	calls := 0
	_, err := withRetry(ctx, fastPolicy(), func() (int, error) { calls++; return 0, nil })
	if calls != 0 {
		t.Errorf("calls = %d, want 0 — a canceled context must short-circuit before calling fn", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}

// TestWithRetry_Property: withRetry calls fn at most maxAttempts times; a
// non-retryable failure is tried exactly once; a run that turns successful
// within the budget returns no error.
func TestWithRetry_Property(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		failures := rapid.IntRange(0, 8).Draw(rt, "failures") // transient failures before success
		maxAttempts := rapid.IntRange(1, 6).Draw(rt, "maxAttempts")
		nonRetryable := rapid.Bool().Draw(rt, "nonRetryable")
		p := retryPolicy{maxAttempts: maxAttempts, baseDelay: time.Microsecond, maxDelay: time.Microsecond}

		calls := 0
		_, err := withRetry(context.Background(), p, func() (int, error) {
			calls++
			if calls > failures {
				return 1, nil
			}
			if nonRetryable {
				return 0, &upcloud.Problem{Status: 400}
			}
			return 0, &upcloud.Problem{Status: 503}
		})

		if calls > maxAttempts {
			rt.Fatalf("calls %d exceeded maxAttempts %d", calls, maxAttempts)
		}
		if nonRetryable && failures > 0 && calls != 1 {
			rt.Fatalf("non-retryable must be tried exactly once, got %d", calls)
		}
		if !nonRetryable && failures < maxAttempts && err != nil {
			rt.Fatalf("should have succeeded within budget (failures=%d, max=%d), got %v", failures, maxAttempts, err)
		}
	})
}

// scriptAPI is a programmable serverAPI for the Create retry/idempotency tests.
type scriptAPI struct {
	createErrs  []error // returned by successive CreateServer calls
	createCalls int
	listServers []upcloud.Server // returned by GetServersWithFilters
	detailsByID map[string]*upcloud.ServerDetails
}

func (s *scriptAPI) CreateServer(_ context.Context, r *request.CreateServerRequest) (*upcloud.ServerDetails, error) {
	i := s.createCalls
	s.createCalls++
	if i < len(s.createErrs) && s.createErrs[i] != nil {
		return nil, s.createErrs[i]
	}
	d := &upcloud.ServerDetails{}
	d.UUID = "created-" + r.Title
	d.Title = r.Title
	return d, nil
}
func (s *scriptAPI) GetServersWithFilters(_ context.Context, _ *request.GetServersWithFiltersRequest) (*upcloud.Servers, error) {
	return &upcloud.Servers{Servers: s.listServers}, nil
}
func (s *scriptAPI) GetServerDetails(_ context.Context, r *request.GetServerDetailsRequest) (*upcloud.ServerDetails, error) {
	if d, ok := s.detailsByID[r.UUID]; ok {
		return d, nil
	}
	return &upcloud.ServerDetails{}, nil
}
func (s *scriptAPI) StopServer(context.Context, *request.StopServerRequest) (*upcloud.ServerDetails, error) {
	return nil, nil
}
func (s *scriptAPI) WaitForServerState(context.Context, *request.WaitForServerStateRequest) (*upcloud.ServerDetails, error) {
	return nil, nil
}
func (s *scriptAPI) DeleteServerAndStorages(context.Context, *request.DeleteServerAndStoragesRequest) error {
	return nil
}
func (s *scriptAPI) GetStorages(context.Context, *request.GetStoragesRequest) (*upcloud.Storages, error) {
	return &upcloud.Storages{}, nil
}

func newScriptClient(s *scriptAPI) *Client {
	return &Client{api: s, retry: fastPolicy()}
}

// TestCreate_RetriesTransientThenSucceeds: a transient 503 on the first create is
// retried; the second create succeeds. No duplicate (the reconcile probe finds
// nothing because the first attempt did not actually create anything).
func TestCreate_RetriesTransientThenSucceeds(t *testing.T) {
	s := &scriptAPI{createErrs: []error{&upcloud.Problem{Status: 503}, nil}}
	c := newScriptClient(s)
	srv, err := c.Create(context.Background(), ServerSpec{Title: "fpu-abc", Labels: map[string]string{"fpu-group": "fpu"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.createCalls != 2 {
		t.Errorf("createCalls = %d, want 2 (one transient retry)", s.createCalls)
	}
	if srv.UUID != "created-fpu-abc" {
		t.Errorf("unexpected server: %+v", srv)
	}
}

// TestCreate_ReconcilesAdoptsExistingOnRetry: the first create "fails" transiently
// but the server was actually created server-side (it shows up in the list with
// the unique title). The retry must ADOPT it via reconcile, NOT create a second.
func TestCreate_ReconcilesAdoptsExistingOnRetry(t *testing.T) {
	existing := upcloud.Server{UUID: "u-1", Title: "fpu-abc"}
	s := &scriptAPI{
		createErrs:  []error{&upcloud.Problem{Status: 503}}, // first create fails transiently...
		listServers: []upcloud.Server{existing},             // ...but the server exists
		detailsByID: map[string]*upcloud.ServerDetails{"u-1": func() *upcloud.ServerDetails {
			d := &upcloud.ServerDetails{}
			d.UUID = "u-1"
			d.Title = "fpu-abc"
			return d
		}()},
	}
	c := newScriptClient(s)
	srv, err := c.Create(context.Background(), ServerSpec{Title: "fpu-abc", Labels: map[string]string{"fpu-group": "fpu"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.createCalls != 1 {
		t.Errorf("createCalls = %d, want 1 — a retry must adopt the existing server, not create a duplicate", s.createCalls)
	}
	if srv.UUID != "u-1" {
		t.Errorf("expected to adopt existing server u-1, got %+v", srv)
	}
}
