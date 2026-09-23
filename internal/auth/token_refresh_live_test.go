//go:build live

package auth

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

// Live cancel coverage against the real Last9 OAuth endpoint. The unit tests
// hold an httptest forever; here we delay the real RoundTrip so a cancel can
// land while the refresh is still in flight. Skip unless LAST9_REFRESH_TOKEN
// (or TEST_REFRESH_TOKEN) is set. Run with:
//
//	go test -tags=live -race -count=1 ./internal/auth/ -run LiveTokenRefreshCancel
func TestLiveTokenRefreshCancel(t *testing.T) {
	loadLiveEnv(t)
	refresh := os.Getenv("TEST_REFRESH_TOKEN")
	if refresh == "" {
		refresh = os.Getenv("LAST9_REFRESH_TOKEN")
	}
	if refresh == "" {
		t.Skip("LAST9_REFRESH_TOKEN / TEST_REFRESH_TOKEN not set")
	}

	const delay = 2 * time.Second
	oauthHits := installOAuthDelay(t, delay)

	tm, err := NewTokenManager(refresh)
	if err != nil {
		t.Fatalf("NewTokenManager (live OAuth): %v", err)
	}
	if tm.AccessToken == "" {
		t.Fatal("live OAuth returned empty access token")
	}
	t.Logf("live OAuth ok; org token acquired (hits so far=%d)", oauthHits.Load())

	// Force the slow path on the next GetAccessToken.
	tm.mu.Lock()
	stale := tm.AccessToken
	tm.ExpiresAt = time.Now().Add(-time.Minute)
	tm.refreshBuffer = 0
	tm.mu.Unlock()

	t.Run("canceled_waiter_returns_promptly", func(t *testing.T) {
		before := oauthHits.Load()
		aDone := make(chan string, 1)
		go func() { aDone <- tm.GetAccessToken(context.Background()) }()

		// Wait until A's refresh has entered the delayed RoundTrip.
		deadline := time.Now().Add(3 * time.Second)
		for oauthHits.Load() == before && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if oauthHits.Load() == before {
			t.Fatal("refresh never reached live OAuth")
		}

		ctxB, cancelB := context.WithCancel(context.Background())
		bDone := make(chan time.Duration, 1)
		go func() {
			start := time.Now()
			tm.GetAccessToken(ctxB)
			bDone <- time.Since(start)
		}()
		time.Sleep(100 * time.Millisecond) // let B park
		cancelB()

		select {
		case elapsed := <-bDone:
			if elapsed > 500*time.Millisecond {
				t.Fatalf("canceled waiter took %v; want <500ms while live refresh is delayed %v", elapsed, delay)
			}
			t.Logf("canceled waiter returned in %v (live refresh still in flight)", elapsed)
		case <-time.After(2 * time.Second):
			t.Fatal("canceled waiter did not return promptly against live OAuth")
		}
		<-aDone // drain A's refresh
	})

	// Re-force expiry for the initiator scenario.
	tm.mu.Lock()
	stale = tm.AccessToken
	tm.ExpiresAt = time.Now().Add(-time.Minute)
	tm.refreshBuffer = 0
	tm.mu.Unlock()

	t.Run("initiator_cancel_does_not_abort_shared_refresh", func(t *testing.T) {
		before := oauthHits.Load()
		ctxA, cancelA := context.WithCancel(context.Background())
		aDone := make(chan string, 1)
		go func() { aDone <- tm.GetAccessToken(ctxA) }()

		deadline := time.Now().Add(3 * time.Second)
		for oauthHits.Load() == before && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if oauthHits.Load() == before {
			t.Fatal("refresh never reached live OAuth")
		}

		bDone := make(chan string, 1)
		go func() { bDone <- tm.GetAccessToken(context.Background()) }()
		time.Sleep(100 * time.Millisecond)
		cancelA()

		select {
		case got := <-bDone:
			if got == "" || got == stale {
				t.Fatalf("uncanceled waiter must receive a refreshed live token; got empty or stale")
			}
			exp, err := GetTokenExpiry(got)
			if err != nil {
				t.Fatalf("waiter token not a valid JWT: %v", err)
			}
			if !exp.After(time.Now()) {
				t.Fatalf("waiter token already expired: %v", exp)
			}
			t.Logf("waiter got fresh live token after initiator cancel (exp=%s)", exp.Format(time.RFC3339))
		case <-time.After(delay + 5*time.Second):
			t.Fatal("uncanceled waiter never returned after live refresh")
		}
		<-aDone
	})
}

func loadLiveEnv(t *testing.T) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	for {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			_ = godotenv.Load(candidate)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// installOAuthDelay wraps the shared HTTP client's transport so each real
// /oauth/access_token call sleeps before continuing. Restored on cleanup.
func installOAuthDelay(t *testing.T, delay time.Duration) *atomic.Int32 {
	t.Helper()
	client := GetHTTPClient()
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	hits := &atomic.Int32{}
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/oauth/access_token") {
			hits.Add(1)
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return base.RoundTrip(r)
	})
	t.Cleanup(func() { client.Transport = base })
	return hits
}
