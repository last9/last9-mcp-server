package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A tool call that is canceled while another caller's refresh is in flight
// must return promptly instead of staying parked until that refresh finishes.
// Schedule: caller A starts a refresh that the server holds open; caller B
// parks on the cond; B's context is canceled; the refresh is still held.
func TestGetAccessToken_CanceledWaiterReturnsPromptly(t *testing.T) {
	newAccessJWT := fakeJWT(t, map[string]interface{}{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	received := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer close(release)

	refreshJWT := fakeJWT(t, map[string]interface{}{
		"aud": []string{server.URL},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	tm := newTestTokenManager(t, "expired", time.Now().Add(-time.Minute), 0)
	tm.RefreshToken = refreshJWT

	aDone := make(chan string, 1)
	go func() { aDone <- tm.GetAccessToken(context.Background()) }()
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh request never reached the server")
	}

	ctxB, cancelB := context.WithCancel(context.Background())
	bDone := make(chan struct{})
	go func() {
		tm.GetAccessToken(ctxB)
		close(bDone)
	}()
	time.Sleep(100 * time.Millisecond) // let B park on the cond
	cancelB()

	select {
	case <-bDone:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled waiter must return promptly while another caller's refresh is in flight")
	}
}
