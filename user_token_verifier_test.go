package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

// fakeClock is a settable now for TTL tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newCountingVerifier(err error) (*userTokenVerifier, *atomic.Int32, *fakeClock) {
	calls := &atomic.Int32{}
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	v := newUserTokenVerifier(func(context.Context, string) error {
		calls.Add(1)
		return err
	})
	v.now = clock.now
	return v, calls, clock
}

func TestUserTokenVerifierCachesSuccessUntilTTL(t *testing.T) {
	v, calls, clock := newCountingVerifier(nil)
	exp := clock.t.Add(time.Hour)
	for range 3 {
		if got := v.verify(context.Background(), "token", exp); got != "ok" {
			t.Fatalf("verify = %q, want ok", got)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1 while cached", calls.Load())
	}
	clock.t = clock.t.Add(userTokenVerifyTTL - time.Second)
	v.verify(context.Background(), "token", exp)
	if calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1 just before the TTL", calls.Load())
	}
	clock.t = clock.t.Add(time.Second)
	v.verify(context.Background(), "token", exp)
	if calls.Load() != 2 {
		t.Fatalf("checks = %d, want a re-check at the TTL", calls.Load())
	}
}

func TestUserTokenVerifierTTLStopsAtTokenExpiry(t *testing.T) {
	v, calls, clock := newCountingVerifier(nil)
	exp := clock.t.Add(10 * time.Second)
	v.verify(context.Background(), "token", exp)
	clock.t = exp.Add(-time.Second)
	v.verify(context.Background(), "token", exp)
	if calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1 before exp", calls.Load())
	}
	clock.t = exp
	v.verify(context.Background(), "token", exp)
	if calls.Load() != 2 {
		t.Fatalf("checks = %d, want a re-check at exp", calls.Load())
	}
}

func TestUserTokenVerifierKeysByToken(t *testing.T) {
	v, calls, clock := newCountingVerifier(nil)
	exp := clock.t.Add(time.Hour)
	v.verify(context.Background(), "token-a", exp)
	v.verify(context.Background(), "token-b", exp)
	if calls.Load() != 2 {
		t.Fatalf("checks = %d, want one per distinct token", calls.Load())
	}
}

func TestUserTokenVerifierClassifiesFailures(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want string
	}{
		{"401", &utils.UpstreamHTTPError{StatusCode: http.StatusUnauthorized}, "unauthorized"},
		{"403 wrapped", fmt.Errorf("verify: %w", &utils.UpstreamHTTPError{StatusCode: http.StatusForbidden}), "unauthorized"},
		{"500", &utils.UpstreamHTTPError{StatusCode: http.StatusInternalServerError}, "verify_failed"},
		{"404", &utils.UpstreamHTTPError{StatusCode: http.StatusNotFound}, "verify_failed"},
		{"transport", errors.New("connection reset"), "verify_failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v, calls, clock := newCountingVerifier(tt.err)
			exp := clock.t.Add(time.Hour)
			for range 2 {
				if got := v.verify(context.Background(), "token", exp); got != tt.want {
					t.Fatalf("verify = %q, want %q", got, tt.want)
				}
			}
			if calls.Load() != 2 {
				t.Fatalf("checks = %d, want failures never cached", calls.Load())
			}
		})
	}
}

// TestUserTokenVerifierSharesInFlightCheck holds the first check open until
// every caller has arrived, so all of them must wait on that one call.
func TestUserTokenVerifierSharesInFlightCheck(t *testing.T) {
	const callers = 8
	release := make(chan struct{})
	calls := &atomic.Int32{}
	v := newUserTokenVerifier(func(context.Context, string) error {
		calls.Add(1)
		<-release
		return nil
	})
	exp := time.Now().Add(time.Hour)
	var wg sync.WaitGroup
	results := make([]string, callers)
	for i := range callers {
		wg.Go(func() { results[i] = v.verify(context.Background(), "token", exp) })
	}
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	// Waiters register before release: the entry is in the map before check runs.
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	for i, got := range results {
		if got != "ok" {
			t.Fatalf("caller %d: verify = %q", i, got)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("checks = %d, want 1 shared check", calls.Load())
	}
}

func TestUserTokenVerifierWaiterHonoursOwnContext(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	v := newUserTokenVerifier(func(context.Context, string) error {
		close(started)
		<-release
		return nil
	})
	defer close(release)
	exp := time.Now().Add(time.Hour)
	go v.verify(context.Background(), "token", exp)
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := v.verify(ctx, "token", exp); got != "verify_failed" {
		t.Fatalf("cancelled waiter got %q, want verify_failed", got)
	}
}

func TestUserTokenVerifierCapsCachedTokens(t *testing.T) {
	v, calls, clock := newCountingVerifier(nil)
	v.limit = 2
	exp := clock.t.Add(time.Hour)
	for _, token := range []string{"a", "b", "c"} {
		v.verify(context.Background(), token, exp)
	}
	if n := len(v.entries); n != 2 {
		t.Fatalf("cached = %d, want the cap of 2", n)
	}
	v.verify(context.Background(), "c", exp)
	if calls.Load() != 4 {
		t.Fatalf("checks = %d, want the over-cap token re-checked", calls.Load())
	}
	// Expired entries are swept on the next successful write, freeing room.
	clock.t = clock.t.Add(userTokenVerifyTTL)
	v.verify(context.Background(), "d", clock.t.Add(time.Hour))
	if _, ok := v.entries[userTokenKey("d")]; !ok || len(v.entries) != 1 {
		t.Fatalf("entries after sweep = %d, want only d", len(v.entries))
	}
}

// TestAPIUserTokenCheck pins the verification call: an org-scoped GET the
// startup datasource cache also needs, sent with the delegated token rather
// than the configured one.
func TestAPIUserTokenCheck(t *testing.T) {
	var gotPath, gotToken string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken = r.Method+" "+r.URL.Path, r.Header.Get(constants.HeaderXLast9APIToken)
		if gotToken != constants.BearerPrefix+"delegated" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer api.Close()
	cfg := models.Config{APIBaseURL: api.URL + "/api/v4/organizations/acme", TokenManager: &auth.TokenManager{AccessToken: "configured", ExpiresAt: time.Now().Add(time.Hour)}}
	check := apiUserTokenCheck(cfg)
	if err := check(context.Background(), "delegated"); err != nil {
		t.Fatalf("check = %v", err)
	}
	if gotPath != "GET /api/v4/organizations/acme"+constants.EndpointDatasources {
		t.Fatalf("request = %q", gotPath)
	}
	var upstream *utils.UpstreamHTTPError
	if err := check(context.Background(), "forged"); !errors.As(err, &upstream) || upstream.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged token check = %v, want a 401 upstream error", err)
	}
	if gotToken != constants.BearerPrefix+"forged" {
		t.Fatalf("token sent = %q, want the delegated one", gotToken)
	}
}
