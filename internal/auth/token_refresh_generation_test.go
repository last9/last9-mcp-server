package auth

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A waiter must return once its refresh completes, even if that refresh failed
// and another caller started refresh 2 before the waiter reacquired condMu.
func TestGetAccessToken_WaiterReturnsAfterItsRefreshEvenIfAnotherStarts(t *testing.T) {
	tm := newTestTokenManager(t, "stale", time.Now().Add(-time.Minute), 0)
	tm.refreshing = true

	done := make(chan string, 1)
	go func() { done <- tm.GetAccessToken(context.Background()) }()
	time.Sleep(50 * time.Millisecond) // let the waiter park in refreshCond.Wait

	tm.condMu.Lock()
	tm.completeRefreshLocked()
	tm.refreshing = true
	tm.condMu.Unlock()

	select {
	case got := <-done:
		if got != "stale" {
			t.Fatalf("want stale token after failed refresh, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter kept waiting on a refresh started after the one it waited for")
	}
}

// A failed refresh leaves callers with a stale token and downstream 401s, so
// it must be logged with its cause, and never with a token.
func TestRefreshToken_LogsFailureWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	refreshJWT := fakeJWT(t, map[string]interface{}{
		"aud": []string{server.URL},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	tm := newTestTokenManager(t, "stale-access", time.Now().Add(-time.Minute), 0)
	tm.RefreshToken = refreshJWT

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	tm.refreshing = true
	tm.refreshToken(context.Background())

	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "unexpected status code: 503") {
		t.Fatalf("want a warning with the refresh error, got %q", out)
	}
	if strings.Contains(out, refreshJWT) || strings.Contains(out, "stale-access") {
		t.Fatalf("refresh failure log must not contain a token: %q", out)
	}
}
