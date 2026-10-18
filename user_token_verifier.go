package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"sync"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

const (
	// userTokenVerifyTTL bounds how long a verified token is trusted without
	// asking the API again, so a revoked token stops working within it.
	userTokenVerifyTTL = time.Minute
	// userTokenVerifyTimeout bounds the verification call; the caller waits.
	userTokenVerifyTimeout = 10 * time.Second
	// maxVerifiedUserTokens caps the cache (~100 B per entry); past it,
	// tokens are still verified, just not cached.
	maxVerifiedUserTokens = 10_000
)

// userTokenVerifier proves a delegated token is accepted by the Last9 API
// before it is bound to a call: its claims are unsigned input, so a token
// with valid claims and a forged signature would otherwise reach tools that
// answer from startup-cached data without any API call. Successes are cached
// by token hash, never by the token itself.
type userTokenVerifier struct {
	check func(ctx context.Context, token string) error
	now   func() time.Time
	limit int

	mu      sync.Mutex
	entries map[[sha256.Size]byte]*tokenVerification
}

// tokenVerification is one cache entry; ready closes once result and expires
// are set, so concurrent callers for a token share one check.
type tokenVerification struct {
	ready   chan struct{}
	result  string
	expires time.Time
}

func newUserTokenVerifier(check func(ctx context.Context, token string) error) *userTokenVerifier {
	return &userTokenVerifier{
		check:   check,
		now:     time.Now,
		limit:   maxVerifiedUserTokens,
		entries: map[[sha256.Size]byte]*tokenVerification{},
	}
}

// apiUserTokenCheck verifies a token with an org-scoped read of the
// datasources the startup cache is built from, sent as that token.
func apiUserTokenCheck(cfg models.Config) func(context.Context, string) error {
	client := last9api.NewClient(last9mcp.WithHTTPTracing(&http.Client{Timeout: userTokenVerifyTimeout}), cfg)
	return func(ctx context.Context, token string) error {
		req := last9api.Request{Method: http.MethodGet, Path: constants.EndpointDatasources}
		return client.Do(auth.WithRequestToken(ctx, token), "verify user token", req, nil)
	}
}

func userTokenKey(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

// verify returns "ok" when the API accepts token, "unauthorized" when it
// refuses it (401/403), and "verify_failed" when it could not tell. A success
// is cached until min(exp, now+userTokenVerifyTTL); failures are not cached.
func (v *userTokenVerifier) verify(ctx context.Context, token string, exp time.Time) string {
	key := userTokenKey(token)
	v.mu.Lock()
	entry := v.entries[key]
	if entry != nil && entry.expired(v.now()) {
		delete(v.entries, key)
		entry = nil
	}
	leader := entry == nil
	if leader {
		entry = &tokenVerification{ready: make(chan struct{})}
		v.entries[key] = entry
	}
	v.mu.Unlock()

	if leader {
		v.run(key, entry, token, exp)
	}
	select {
	case <-entry.ready:
		return entry.result
	case <-ctx.Done():
		return "verify_failed"
	}
}

// run performs the check outside the lock. It ignores the caller's
// cancellation so one caller going away never fails others waiting on it.
func (v *userTokenVerifier) run(key [sha256.Size]byte, entry *tokenVerification, token string, exp time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), userTokenVerifyTimeout)
	defer cancel()
	result := classifyUserTokenCheck(v.check(ctx, token))

	v.mu.Lock()
	entry.result = result
	if result == "ok" {
		now := v.now()
		entry.expires = now.Add(userTokenVerifyTTL)
		if exp.Before(entry.expires) {
			entry.expires = exp
		}
		v.sweep(now)
		if len(v.entries) > v.limit {
			delete(v.entries, key)
		}
	} else {
		delete(v.entries, key)
	}
	v.mu.Unlock()
	close(entry.ready)
}

// sweep drops expired entries; callers hold mu.
func (v *userTokenVerifier) sweep(now time.Time) {
	for key, entry := range v.entries {
		if entry.expired(now) {
			delete(v.entries, key)
		}
	}
}

// expired reports whether a settled entry is past its expiry; pending
// entries never are. Callers hold mu, which orders the read of expires.
func (e *tokenVerification) expired(now time.Time) bool {
	select {
	case <-e.ready:
		return !now.Before(e.expires)
	default:
		return false
	}
}

func classifyUserTokenCheck(err error) string {
	var upstream *utils.UpstreamHTTPError
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &upstream) && (upstream.StatusCode == http.StatusUnauthorized || upstream.StatusCode == http.StatusForbidden):
		return "unauthorized"
	default:
		return "verify_failed"
	}
}
