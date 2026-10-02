package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The shared refresh must not inherit the lifetime of whichever tool call
// happened to start it. Schedule: the token has expired; caller A starts the
// refresh, which the server holds open; caller B (not canceled) parks on the
// cond; A's context is canceled; then the server answers.
func TestGetAccessToken_InitiatorCancelDoesNotAbortSharedRefresh(t *testing.T) {
	newAccessJWT := fakeJWT(t, map[string]interface{}{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	var hits atomic.Int32
	received := make(chan struct{}, 4)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		received <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": newAccessJWT})
	}))
	defer server.Close()

	refreshJWT := fakeJWT(t, map[string]interface{}{
		"aud": []string{server.URL},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	tm := newTestTokenManager(t, "expired", time.Now().Add(-time.Minute), 0)
	tm.RefreshToken = refreshJWT

	ctxA, cancelA := context.WithCancel(context.Background())
	go tm.GetAccessToken(ctxA)
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh request never reached the server")
	}

	bDone := make(chan string, 1)
	go func() { bDone <- tm.GetAccessToken(context.Background()) }()
	time.Sleep(100 * time.Millisecond) // let B park on the cond
	cancelA()
	time.Sleep(100 * time.Millisecond) // let A's cancellation propagate
	close(release)

	select {
	case got := <-bDone:
		if got != newAccessJWT {
			t.Fatalf("uncanceled waiter must receive the refreshed token after the initiating caller is canceled, got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("uncanceled waiter never returned")
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("want exactly 1 refresh HTTP call, got %d", n)
	}
}

// Detaching the refresh from its initiator must not make that initiator wait
// for it: a canceled initiator still returns promptly while the server holds
// the refresh open.
func TestGetAccessToken_CanceledInitiatorReturnsPromptly(t *testing.T) {
	received := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		http.Error(w, "held", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	defer close(release)

	refreshJWT := fakeJWT(t, map[string]interface{}{
		"aud": []string{server.URL},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	tm := newTestTokenManager(t, "expired", time.Now().Add(-time.Minute), 0)
	tm.RefreshToken = refreshJWT

	ctxA, cancelA := context.WithCancel(context.Background())
	aDone := make(chan struct{})
	go func() {
		tm.GetAccessToken(ctxA)
		close(aDone)
	}()
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh request never reached the server")
	}
	cancelA()
	select {
	case <-aDone:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled initiator must return promptly while its refresh is held")
	}
}
