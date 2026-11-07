package auth

import (
	"context"
	"testing"
	"time"
)

// A waiter must return once the refresh it waited on completes, even if that
// refresh failed and another caller immediately started the next one.
// Schedule: refresh 1 is in flight; the waiter parks; refresh 1 fails and
// broadcasts; before the waiter reacquires condMu, caller B starts refresh 2.
func TestGetAccessToken_WaiterReturnsAfterItsRefreshEvenIfAnotherStarts(t *testing.T) {
	tm := newTestTokenManager(t, "stale", time.Now().Add(-time.Minute), 0)
	tm.refreshing = true

	done := make(chan string, 1)
	go func() { done <- tm.GetAccessToken(context.Background()) }()
	time.Sleep(50 * time.Millisecond) // let the waiter park in refreshCond.Wait

	tm.condMu.Lock()
	tm.refreshing = false
	tm.refreshCond.Broadcast()
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
