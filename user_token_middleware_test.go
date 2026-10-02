package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testUserJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestUserTokenMiddleware(t *testing.T) {
	valid := testUserJWT(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "aud": []string{"https://api.example.test/path"}})
	other := testUserJWT(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "aud": []string{"api.example.test"}})
	expired := testUserJWT(t, map[string]any{"exp": time.Now().Add(-time.Hour).Unix(), "aud": []string{"api.example.test"}})
	wrongAud := testUserJWT(t, map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "aud": []string{"elsewhere.test"}})
	tests := []struct {
		name, method, meta, header, wantToken, wantError string
		shadow                                           bool
	}{
		{"other method", "tools/list", "", "", "", "", false},
		{"other method ignores bad token", "tools/list", "bad-token", "", "", "", false},
		{"meta", "tools/call", valid, "", valid, "", false},
		{"header", "tools/call", "", other, other, "", false},
		{"meta wins", "tools/call", valid, other, valid, "", false},
		{"missing", "tools/call", "", "", "", "user token required", false},
		{"expired", "tools/call", expired, "", "", "expired", false},
		{"malformed", "tools/call", "bad-token", "", "", "malformed", false},
		{"audience", "tools/call", wrongAud, "", "", "audience_mismatch", false},
		{"shadow valid", "tools/call", valid, "", "", "", true},
		{"shadow missing", "tools/call", "", "", "", "", true},
		{"shadow malformed", "tools/call", "bad-token", "", "", "", true},
		{"shadow expired", "tools/call", expired, "", "", "", true},
		{"shadow audience", "tools/call", wrongAud, "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			if tt.shadow {
				original := log.Writer()
				log.SetOutput(&logs)
				defer log.SetOutput(original)
			}
			cfg := models.Config{APIBaseURL: "https://api.example.test/api/v4/organizations/test", UserTokenFallback: tt.shadow}
			called := false
			mw := userTokenMiddleware(cfg)
			h := mw(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
				called = true
				got, _ := auth.RequestTokenFromContext(ctx)
				if got != tt.wantToken {
					t.Errorf("context token = %q, want %q", got, tt.wantToken)
				}
				return &mcp.CallToolResult{}, nil
			})
			params := &mcp.CallToolParamsRaw{Name: "probe"}
			if tt.meta != "" {
				params.Meta = mcp.Meta{"last9_user_token": tt.meta}
			}
			req := &mcp.CallToolRequest{Params: params, Extra: &mcp.RequestExtra{Header: http.Header{}}}
			if tt.header != "" {
				req.Extra.Header.Set("Authorization", "Bearer "+tt.header)
			}
			_, err := h(context.Background(), tt.method, req)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) || called {
					t.Fatalf("error = %v, called = %v", err, called)
				}
			} else if err != nil || !called {
				t.Fatalf("error = %v, called = %v", err, called)
			}
			if tt.shadow {
				wantResult := "ok"
				switch tt.name {
				case "shadow missing":
					wantResult = "missing"
				case "shadow malformed":
					wantResult = "malformed"
				case "shadow expired":
					wantResult = "expired"
				case "shadow audience":
					wantResult = "audience_mismatch"
				}
				if got := logs.String(); strings.Count(got, "user_token_shadow") != 1 || !strings.Contains(got, "user_token_shadow result="+wantResult+" method=tools/call tool=probe") || (tt.meta != "" && strings.Contains(got, tt.meta)) {
					t.Fatal("shadow log did not match the sanitized result line")
				}
			}
		})
	}
}

func TestUserTokenFallbackFromEnv(t *testing.T) {
	t.Setenv("LAST9_MCP_USER_TOKEN_FALLBACK", "")
	if err := os.Unsetenv("LAST9_MCP_USER_TOKEN_FALLBACK"); err != nil {
		t.Fatal(err)
	}
	if !userTokenFallbackFromEnv() {
		t.Fatal("unset flag must enable shadow mode")
	}
	for _, tt := range []struct {
		value string
		want  bool
	}{
		{"", true}, {"false", false}, {"FALSE", false}, {"0", false}, {"no", false}, {"true", true}, {"other", true},
	} {
		t.Setenv("LAST9_MCP_USER_TOKEN_FALLBACK", tt.value)
		if got := userTokenFallbackFromEnv(); got != tt.want {
			t.Errorf("value %q: got %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestSafeToolName(t *testing.T) {
	if safeToolName("get_logs") != "get_logs" || safeToolName("get_logs\nsecret") != "unknown" || safeToolName("a.b.c") != "unknown" {
		t.Fatal("tool name logging accepted unsafe input")
	}
}
