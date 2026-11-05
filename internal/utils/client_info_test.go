package utils

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestClientSessionIDRoundTripsBoundedIdentity(t *testing.T) {
	for _, n := range []int{99, 100, 101} {
		name, version := strings.Repeat("é", n), strings.Repeat("v", n)
		id := NewClientSessionID(mcp.Implementation{Name: name, Version: version})
		info := RequestClientInfo(WithClientSession(context.Background(), id), nil)
		want := min(n, maxClientInfoRunes)
		if info == nil || info.Name != strings.Repeat("é", want) || info.Version != strings.Repeat("v", want) {
			t.Errorf("%d characters: got %+v, want both cut to %d", n, info, want)
		}
	}
	if a, b := NewClientSessionID(mcp.Implementation{Name: "c"}), NewClientSessionID(mcp.Implementation{Name: "c"}); a == b {
		t.Errorf("session IDs for the same client must differ, both %q", a)
	}
}

func TestWithClientSessionIgnoresMalformedIDs(t *testing.T) {
	for _, id := range []string{"", "abc123", "l9c1-", "l9c1-nonce", "l9c1-nonce.!!!", "l9c1-nonce.bnVsbA", "l9c1-nonce.e30"} {
		if info := RequestClientInfo(WithClientSession(context.Background(), id), nil); info != nil {
			t.Errorf("%q: got %+v, want no client identity", id, info)
		}
	}
}

// Session IDs are unsigned, so a caller can encode any identity; it is self-reported, like
// clientInfo, and the README says so.
func TestWithClientSessionTakesWellFormedCallerIDsAsSelfReported(t *testing.T) {
	id := clientSessionPrefix + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"name":"fabricated","version":"1"}`))
	info := RequestClientInfo(WithClientSession(context.Background(), id), nil)
	if info == nil || info.Name != "fabricated" || info.Version != "1" {
		t.Errorf("got %+v, want the caller-encoded identity fabricated 1", info)
	}
}
