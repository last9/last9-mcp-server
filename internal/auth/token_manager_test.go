package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeJWT builds an unsigned JWT whose payload decodes to the given claims.
// ExtractClaimsFromToken only base64-decodes the payload (parts[1]); it does
// not verify the signature, so an empty signature is sufficient. The token
// ends with a trailing "." so strings.Split yields exactly 3 parts.
func fakeJWT(t *testing.T, claims map[string]interface{}) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + "."
}

// newTestTokenManager builds a TokenManager with refreshCond wired to condMu
// (the production invariant) but without the OAuth round-trip NewTokenManager
// performs, so tests can drive the slow path deterministically.
func newTestTokenManager(t *testing.T, accessToken string, expiresAt time.Time, refreshBuffer time.Duration) *TokenManager {
	t.Helper()
	tm := &TokenManager{
		AccessToken:   accessToken,
		ExpiresAt:     expiresAt,
		refreshBuffer: refreshBuffer,
	}
	tm.refreshCond = sync.NewCond(&tm.condMu)
	return tm
}

// simulateRefreshCompletion mirrors what refreshToken's success path does:
// update the token fields under mu, then clear refreshing + broadcast under
// condMu. It lets concurrency tests drive the "refresh completes" signal
// without hitting the network.
func (tm *TokenManager) simulateRefreshCompletion(newToken string, newExpiry time.Time) {
	tm.mu.Lock()
	tm.AccessToken = newToken
	tm.ExpiresAt = newExpiry
	tm.mu.Unlock()

	tm.condMu.Lock()
	tm.refreshing = false
	tm.refreshCond.Broadcast()
	tm.condMu.Unlock()
}

// TestGetAccessToken_FastPathReturnsCachedToken confirms that when the token
// is still within its refresh buffer, GetAccessToken returns the cached
// token immediately without entering the cond/refresh path.
func TestGetAccessToken_FastPathReturnsCachedToken(t *testing.T) {
	tm := newTestTokenManager(t, "cached", time.Now().Add(time.Hour), 0)

	start := time.Now()
	got := tm.GetAccessToken(context.Background())
	elapsed := time.Since(start)

	if got != "cached" {
		t.Fatalf("want cached token %q, got %q", "cached", got)
	}
	if elapsed > time.Second {
		t.Fatalf("fast path should return immediately, took %v", elapsed)
	}
	tm.condMu.Lock()
	refreshing := tm.refreshing
	tm.condMu.Unlock()
	if refreshing {
		t.Fatal("fast path must not mark a refresh in flight")
	}
}

// TestGetAccessToken_DoesNotCrashWhenRefreshInFlight is the direct regression
// for the bug: GetAccessToken must not fatal the process when it reaches the
// `for tm.refreshing { Wait() }` branch. Pre-fix, refreshCond was bound to
// tm.mu (a *sync.RWMutex) and the caller held only an RLock, so Wait()'s
// internal write-Unlock triggered `fatal error: sync: Unlock of unlocked
// RWMutex`. Post-fix, the cond is bound to condMu and the slow path holds
// condMu, so Wait() is correct.
func TestGetAccessToken_DoesNotCrashWhenRefreshInFlight(t *testing.T) {
	// refreshBuffer > remaining life forces the slow path even though the
	// token has not literally expired yet.
	tm := newTestTokenManager(t, "stale", time.Now().Add(2*time.Second), 10*time.Second)
	tm.condMu.Lock()
	tm.refreshing = true
	tm.condMu.Unlock()

	go func() {
		time.Sleep(50 * time.Millisecond)
		tm.condMu.Lock()
		tm.refreshing = false
		tm.refreshCond.Broadcast()
		tm.condMu.Unlock()
	}()

	got := tm.GetAccessToken(context.Background())
	if got != "stale" {
		t.Fatalf("want stale token while refresh is nominally in flight, got %q", got)
	}
}

// TestGetAccessToken_WaitsForRefreshAndReturnsNewToken verifies the intended
// slow-path behaviour: a caller that arrives while a refresh is in flight
// blocks until the refresh completes, then observes the refreshed token.
func TestGetAccessToken_WaitsForRefreshAndReturnsNewToken(t *testing.T) {
	tm := newTestTokenManager(t, "stale", time.Now().Add(-time.Minute), 0)
	tm.condMu.Lock()
	tm.refreshing = true
	tm.condMu.Unlock()

	newExpiry := time.Now().Add(time.Hour)
	go func() {
		time.Sleep(50 * time.Millisecond)
		tm.simulateRefreshCompletion("fresh", newExpiry)
	}()

	start := time.Now()
	got := tm.GetAccessToken(context.Background())
	elapsed := time.Since(start)

	if got != "fresh" {
		t.Fatalf("want refreshed token %q, got %q", "fresh", got)
	}
	if elapsed < 40*time.Millisecond {
		t.Fatalf("GetAccessToken should have blocked for the refresh, took %v", elapsed)
	}
	tm.mu.RLock()
	exp := tm.ExpiresAt
	tm.mu.RUnlock()
	if !exp.Equal(newExpiry) {
		t.Fatalf("ExpiresAt not updated: want %v, got %v", newExpiry, exp)
	}
}

// TestGetAccessToken_ConcurrentWaitersAllReturnNewToken drives the crashing
// branch from many goroutines at once. Pre-fix this would fatal the runtime
// (the fatal fires on the first Wait() under RLock, regardless of how many
// waiters). Post-fix all callers block on condMu, get the refreshed token,
// and the test is clean under -race.
func TestGetAccessToken_ConcurrentWaitersAllReturnNewToken(t *testing.T) {
	const waiters = 50
	tm := newTestTokenManager(t, "stale", time.Now().Add(-time.Minute), 0)
	tm.condMu.Lock()
	tm.refreshing = true
	tm.condMu.Unlock()

	go func() {
		time.Sleep(50 * time.Millisecond)
		tm.simulateRefreshCompletion("fresh", time.Now().Add(time.Hour))
	}()

	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex // guards results (shared across waiter goroutines; -race)
	results := make([]string, 0, waiters)
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			<-start
			got := tm.GetAccessToken(context.Background())
			mu.Lock()
			results = append(results, got)
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if len(results) != waiters {
		t.Fatalf("want %d results, got %d", waiters, len(results))
	}
	for i, r := range results {
		if r != "fresh" {
			t.Fatalf("waiter %d got %q, want %q", i, r, "fresh")
		}
	}
}

// TestGetAccessToken_TriggersRefreshAndReturnsNewToken is an end-to-end check
// through the real RefreshAccessToken path (against an httptest server): an
// expired token with no refresh in flight causes GetAccessToken to trigger
// exactly one refresh, and the caller observes the new token.
func TestGetAccessToken_TriggersRefreshAndReturnsNewToken(t *testing.T) {
	newAccessJWT := fakeJWT(t, map[string]interface{}{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/oauth/access_token" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": newAccessJWT})
	}))
	defer server.Close()

	refreshJWT := fakeJWT(t, map[string]interface{}{
		"aud": []string{server.URL},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	tm := newTestTokenManager(t, "stale", time.Now().Add(-time.Minute), 0)
	tm.RefreshToken = refreshJWT

	got := tm.GetAccessToken(context.Background())
	if got != newAccessJWT {
		t.Fatalf("want refreshed access token, got %q", got)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("want exactly 1 refresh HTTP call, got %d", n)
	}
	tm.condMu.Lock()
	refreshing := tm.refreshing
	tm.condMu.Unlock()
	if refreshing {
		t.Fatal("refreshing must be cleared after refresh completes")
	}
}

// TestGetAccessToken_ConcurrentCallersTriggerSingleRefresh verifies the
// duplicate-refresh race fix end-to-end: when many callers arrive at once for
// an expired token, exactly one refresh HTTP call is made and every caller
// observes the refreshed token. The server delays its response so that all
// callers arrive while the refresh is in flight (refreshing == true), which is
// exactly the state that crashed pre-fix.
func TestGetAccessToken_ConcurrentCallersTriggerSingleRefresh(t *testing.T) {
	const callers = 50
	newAccessJWT := fakeJWT(t, map[string]interface{}{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// Hold the refresh window open long enough that every caller has
		// entered GetAccessToken and parked on the cond before the refresh
		// completes.
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": newAccessJWT})
	}))
	defer server.Close()

	refreshJWT := fakeJWT(t, map[string]interface{}{
		"aud": []string{server.URL},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	tm := newTestTokenManager(t, "stale", time.Now().Add(-time.Minute), 0)
	tm.RefreshToken = refreshJWT

	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex // guards results (-race)
	results := make([]string, 0, callers)
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			<-start
			got := tm.GetAccessToken(context.Background())
			mu.Lock()
			results = append(results, got)
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if len(results) != callers {
		t.Fatalf("want %d results, got %d", callers, len(results))
	}
	for i, r := range results {
		if r != newAccessJWT {
			t.Fatalf("caller %d got %q, want refreshed token", i, r)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("want exactly 1 refresh HTTP call across %d callers, got %d", callers, n)
	}
}

// TestGetAccessToken_FailedRefreshReturnsStaleTokenAndDoesNotHang verifies
// that when RefreshAccessToken fails (upstream 5xx), refreshToken's defer
// still clears refreshing and broadcasts so waiters do not block forever, and
// GetAccessToken returns the existing (stale) token rather than crashing.
func TestGetAccessToken_FailedRefreshReturnsStaleTokenAndDoesNotHang(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream down", http.StatusInternalServerError)
	}))
	defer server.Close()

	refreshJWT := fakeJWT(t, map[string]interface{}{
		"aud": []string{server.URL},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	tm := newTestTokenManager(t, "stale", time.Now().Add(-time.Minute), 0)
	tm.RefreshToken = refreshJWT

	done := make(chan string, 1)
	go func() {
		done <- tm.GetAccessToken(context.Background())
	}()
	select {
	case got := <-done:
		if got != "stale" {
			t.Fatalf("want stale token after failed refresh, got %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GetAccessToken hung after failed refresh; refreshing was not cleared/broadcast")
	}
	tm.condMu.Lock()
	refreshing := tm.refreshing
	tm.condMu.Unlock()
	if refreshing {
		t.Fatal("refreshing must be cleared after a failed refresh so waiters do not block")
	}
}

// TestRefreshToken_BackgroundRefreshPathNoDeadlock mirrors the backgroundRefresh
// call shape: it sets refreshing under condMu, releases condMu, then calls
// refreshToken synchronously. refreshToken's defer re-acquires condMu to clear
// refreshing and broadcast. This test pins that the synchronous caller path
// does not re-enter condMu while already holding it (which would self-deadlock)
// and that the token is updated end-to-end.
func TestRefreshToken_BackgroundRefreshPathNoDeadlock(t *testing.T) {
	newAccessJWT := fakeJWT(t, map[string]interface{}{
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": newAccessJWT})
	}))
	defer server.Close()

	refreshJWT := fakeJWT(t, map[string]interface{}{
		"aud": []string{server.URL},
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	tm := newTestTokenManager(t, "stale", time.Now().Add(-time.Minute), 0)
	tm.RefreshToken = refreshJWT

	// Mirror backgroundRefresh: set refreshing under condMu, release, then
	// call refreshToken synchronously.
	tm.condMu.Lock()
	tm.refreshing = true
	tm.condMu.Unlock()

	done := make(chan struct{})
	go func() {
		tm.refreshToken(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("refreshToken deadlocked (condMu re-entry)")
	}

	tm.condMu.Lock()
	refreshing := tm.refreshing
	tm.condMu.Unlock()
	if refreshing {
		t.Fatal("refreshing must be cleared after synchronous refreshToken")
	}
	if got := tm.GetAccessToken(context.Background()); got != newAccessJWT {
		t.Fatalf("want refreshed token after synchronous refresh, got %q", got)
	}
}
