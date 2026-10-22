package utils

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/constants"
)

// stalledLogSearchServer accepts the search and never finishes answering it.
// With writeHeaders the stall happens mid-body instead of before the headers.
func stalledLogSearchServer(t *testing.T, writeHeaders bool) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if writeHeaders {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"query_result":`))
			w.(http.Flusher).Flush()
		}
		arrived <- struct{}{}
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })
	return srv, arrived
}

func searchLogsWithBudget(t *testing.T, ctx context.Context, srv *httptest.Server, budget time.Duration) (time.Duration, error) {
	t.Helper()
	cfg := logSearchTestCfg(t, srv.URL)
	cfg.LogSearchTimeout = budget
	started := time.Now()
	_, err := SearchLogs(ctx, srv.Client(), cfg, LogSearchRequest{
		Op:       "logs query",
		Pipeline: []map[string]any{{"type": "filter"}},
		StartMs:  1_700_000_000_000,
		EndMs:    1_700_000_600_000,
	})
	return time.Since(started), err
}

func assertLogSearchTimeoutAdvice(t *testing.T, err error, elapsed time.Duration) {
	t.Helper()
	if !errors.Is(err, ErrLogSearchTimeout) {
		t.Fatalf("err = %v, want ErrLogSearchTimeout", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("returned after %s, want within the 50ms budget", elapsed)
	}
	msg := err.Error()
	for _, want := range []string{
		"logs query timed out after 50ms",
		"Try narrowing the time range",
		"adding a service filter",
		"Do not retry the same request unchanged",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q is missing %q", msg, want)
		}
	}
	if strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "Client.Timeout") {
		t.Errorf("error %q leaks the raw transport error", msg)
	}
}

func TestSearchLogs_StalledBeforeHeadersReturnsAdviceWithinBudget(t *testing.T) {
	srv, _ := stalledLogSearchServer(t, false)
	elapsed, err := searchLogsWithBudget(t, context.Background(), srv, 50*time.Millisecond)
	assertLogSearchTimeoutAdvice(t, err, elapsed)
}

// The budget covers reading the body too, not only waiting for headers.
func TestSearchLogs_StalledBodyReturnsAdviceWithinBudget(t *testing.T) {
	srv, _ := stalledLogSearchServer(t, true)
	elapsed, err := searchLogsWithBudget(t, context.Background(), srv, 50*time.Millisecond)
	assertLogSearchTimeoutAdvice(t, err, elapsed)
}

// A caller that gave up (session closed, tool cancelled) did not hit the
// budget, so it must not be told to narrow its query.
func TestSearchLogs_CallerCancelIsNotReportedAsTimeout(t *testing.T) {
	srv, arrived := stalledLogSearchServer(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-arrived
		cancel()
	}()

	_, err := searchLogsWithBudget(t, ctx, srv, time.Minute)
	if err == nil {
		t.Fatal("SearchLogs succeeded on a cancelled call")
	}
	if errors.Is(err, ErrLogSearchTimeout) {
		t.Errorf("err = %v, want a cancellation, not the time budget advice", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
}

func TestLogSearchBudget(t *testing.T) {
	cfg := logSearchTestCfg(t, "http://unused")
	if got := logSearchBudget(cfg); got != constants.LogSearchTimeout {
		t.Errorf("unset budget = %s, want constants.LogSearchTimeout (%s)", got, constants.LogSearchTimeout)
	}
	if constants.LogSearchTimeout >= constants.DefaultHTTPTimeout {
		t.Errorf("LogSearchTimeout %s must stay under the shared client timeout %s",
			constants.LogSearchTimeout, constants.DefaultHTTPTimeout)
	}
	cfg.LogSearchTimeout = 7 * time.Second
	if got := logSearchBudget(cfg); got != 7*time.Second {
		t.Errorf("configured budget = %s, want 7s", got)
	}
}
