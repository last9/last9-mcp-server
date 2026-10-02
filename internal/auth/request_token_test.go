package auth

import (
	"context"
	"testing"
	"time"
)

func TestRequestToken(t *testing.T) {
	if _, ok := RequestTokenFromContext(context.Background()); ok {
		t.Fatal("unexpected token")
	}
	ctx := WithRequestToken(context.Background(), "request-token")
	if got, ok := RequestTokenFromContext(ctx); !ok || got != "request-token" {
		t.Fatalf("token = %q, %v", got, ok)
	}
	tm := newTestTokenManager(t, "pod-token", time.Time{}, 0)
	if got := tm.GetAccessToken(ctx); got != "request-token" {
		t.Fatalf("GetAccessToken = %q", got)
	}
	if tm.refreshing {
		t.Fatal("request token triggered refresh")
	}
}
