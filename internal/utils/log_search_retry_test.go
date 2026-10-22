package utils

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

const okLogSearchBody = `{"query_result":{"status":"success","data":{"resultType":"streams","result":[]}}}`

// sequencedLogSearchServer answers the nth search with statuses[n], repeating
// the last status once the list runs out. answered receives one value per search.
func sequencedLogSearchServer(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32, <-chan struct{}) {
	t.Helper()
	var calls atomic.Int32
	answered := make(chan struct{}, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		n := int(calls.Add(1)) - 1
		defer func() { answered <- struct{}{} }()
		status := statuses[min(n, len(statuses)-1)]
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(okLogSearchBody))
			return
		}
		_, _ = w.Write([]byte(`{"error":"upstream"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, answered
}

func withLogSearchRetryDelay(t *testing.T, d time.Duration) {
	t.Helper()
	old := logSearchRetryDelay
	logSearchRetryDelay = d
	t.Cleanup(func() { logSearchRetryDelay = old })
}

func runSearchLogs(ctx context.Context, t *testing.T, srv *httptest.Server) error {
	t.Helper()
	_, err := SearchLogs(ctx, srv.Client(), logSearchTestCfg(t, srv.URL), LogSearchRequest{
		Op:       "logs query",
		Pipeline: []map[string]any{{"type": "filter"}},
		StartMs:  1_700_000_000_000,
		EndMs:    1_700_000_600_000,
	})
	return err
}

func TestSearchLogs_RetriesTransientGatewayStatusOnce(t *testing.T) {
	withLogSearchRetryDelay(t, 0)
	for _, status := range []int{http.StatusBadGateway, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, calls, _ := sequencedLogSearchServer(t, status, http.StatusOK)
			ctx, ended := RecordedSpanContext(t)

			if err := runSearchLogs(ctx, t, srv); err != nil {
				t.Fatalf("SearchLogs after one %d: %v", status, err)
			}
			if got := calls.Load(); got != 2 {
				t.Errorf("searches = %d, want 2", got)
			}
			attrs := ended()
			for key, want := range map[attribute.Key]attribute.Value{
				attrLogSearchAttempts:      attribute.IntValue(2),
				attrLogSearchRetriedStatus: attribute.IntValue(status),
				attrLogUpstreamStatus:      attribute.IntValue(http.StatusOK),
			} {
				if got := attrs[key]; got != want {
					t.Errorf("span %s = %v, want %v", key, got.Emit(), want.Emit())
				}
			}
		})
	}
}

func TestSearchLogs_RetriesAtMostOnce(t *testing.T) {
	withLogSearchRetryDelay(t, 0)
	srv, calls, _ := sequencedLogSearchServer(t, http.StatusBadGateway)
	ctx, ended := RecordedSpanContext(t)

	err := runSearchLogs(ctx, t, srv)
	var upstream *UpstreamHTTPError
	if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusBadGateway {
		t.Fatalf("err = %v, want the second 502", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("searches = %d, want 2", got)
	}
	if got := ended()[attrLogUpstreamStatus]; got != attribute.IntValue(http.StatusBadGateway) {
		t.Errorf("span upstream_status = %v, want 502", got.Emit())
	}
}

// 500 is usually the query itself failing, and 504 a sweep that outlived the
// gateway; rerunning either unchanged repeats the failure at twice the latency.
func TestSearchLogs_DoesNotRetryOtherStatuses(t *testing.T) {
	withLogSearchRetryDelay(t, 0)
	for _, status := range []int{
		http.StatusBadRequest, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusGatewayTimeout,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv, calls, _ := sequencedLogSearchServer(t, status, http.StatusOK)
			ctx, ended := RecordedSpanContext(t)

			if err := runSearchLogs(ctx, t, srv); err == nil {
				t.Fatalf("a %d must fail the call", status)
			}
			if got := calls.Load(); got != 1 {
				t.Errorf("searches = %d, want 1", got)
			}
			attrs := ended()
			if got := attrs[attrLogSearchAttempts]; got != attribute.IntValue(1) {
				t.Errorf("span search_attempts = %v, want 1", got.Emit())
			}
			if _, ok := attrs[attrLogSearchRetriedStatus]; ok {
				t.Error("span has retried_status without a retry")
			}
		})
	}
}

func TestSearchLogs_CancelDuringRetryDelayStopsWithoutRetrying(t *testing.T) {
	withLogSearchRetryDelay(t, time.Hour)
	srv, calls, answered := sequencedLogSearchServer(t, http.StatusServiceUnavailable, http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-answered
		cancel()
	}()

	err := runSearchLogs(ctx, t, srv)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("searches = %d, want 1", got)
	}
}
