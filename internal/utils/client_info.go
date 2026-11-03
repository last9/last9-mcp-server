package utils

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Stateless --http forgets initialize, so the session ID it hands out carries the client's
// name and version itself: any replica can read them back from the ID the client echoes.
const (
	clientSessionPrefix = "l9c1-"
	maxClientInfoRunes  = 100
)

type clientSessionKey struct{}

// NewClientSessionID returns a unique session ID that encodes info's name and version, each
// cut to 100 characters to bound the header.
func NewClientSessionID(info mcp.Implementation) string {
	payload, _ := json.Marshal(mcp.Implementation{
		Name:    truncateRunes(info.Name, maxClientInfoRunes),
		Version: truncateRunes(info.Version, maxClientInfoRunes),
	})
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	return clientSessionPrefix + hex.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(payload)
}

// WithClientSession attaches the client identity encoded in sessionID to ctx. IDs this
// server did not issue leave ctx unchanged.
func WithClientSession(ctx context.Context, sessionID string) context.Context {
	rest, ok := strings.CutPrefix(sessionID, clientSessionPrefix)
	if !ok {
		return ctx
	}
	_, encoded, ok := strings.Cut(rest, ".")
	if !ok {
		return ctx
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ctx
	}
	var info mcp.Implementation
	if json.Unmarshal(payload, &info) != nil || info.Name == "" {
		return ctx
	}
	info.Name, info.Version = truncateRunes(info.Name, maxClientInfoRunes), truncateRunes(info.Version, maxClientInfoRunes)
	return context.WithValue(ctx, clientSessionKey{}, &info)
}

// RequestClientInfo returns the calling client's identity: the request's own clientInfo
// (stdio session or per-request _meta), else the one its --http session ID carries.
func RequestClientInfo(ctx context.Context, req *mcp.CallToolRequest) *mcp.Implementation {
	if req != nil {
		if info := req.ClientInfo(); info != nil {
			return info
		}
	}
	info, _ := ctx.Value(clientSessionKey{}).(*mcp.Implementation)
	return info
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
