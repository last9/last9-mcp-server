package main

import (
	"context"
	"errors"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func userTokenFallbackFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LAST9_MCP_USER_TOKEN_FALLBACK"))) {
	case "false", "0", "no":
		return false
	default:
		return true
	}
}

func userTokenMiddleware(cfg models.Config) mcp.Middleware {
	expectedHost := tokenHost(cfg.APIBaseURL)
	if expectedHost == "" {
		expectedHost = tokenHost(cfg.APIHost)
	}
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			call, ok := req.(*mcp.CallToolRequest)
			if !ok || call.Params == nil {
				return nil, errors.New("malformed user token request")
			}
			token, present, malformed := requestUserToken(call)
			result := "missing"
			switch {
			case malformed:
				result = "malformed"
			case present:
				result = validateUserToken(token, expectedHost)
			}
			if cfg.UserTokenFallback {
				log.Printf("user_token_shadow result=%s method=tools/call tool=%s", result, safeToolName(call.Params.Name))
				return next(ctx, method, req)
			}
			if result == "missing" {
				return nil, errors.New("user token required")
			}
			if result != "ok" {
				return nil, errors.New(result)
			}
			return next(auth.WithRequestToken(ctx, token), method, req)
		}
	}
}

func safeToolName(name string) string {
	if name == "" || len(name) > 128 {
		return "unknown"
	}
	for _, r := range name {
		if r != '_' && r != '-' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return "unknown"
		}
	}
	return name
}

func requestUserToken(req *mcp.CallToolRequest) (string, bool, bool) {
	if value, ok := req.Params.Meta["last9_user_token"]; ok {
		token, ok := value.(string)
		return token, true, !ok || token == ""
	}
	if req.Extra == nil {
		return "", false, false
	}
	header := req.Extra.Header.Get("Authorization")
	if header == "" {
		return "", false, false
	}
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) != token || token == "" {
		return "", true, true
	}
	return token, true, false
}

func validateUserToken(token, expectedHost string) string {
	claims, err := auth.ExtractClaimsFromToken(token)
	if err != nil {
		return "malformed"
	}
	exp, ok := claims["exp"].(float64)
	if !ok || exp <= 0 {
		return "malformed"
	}
	if exp <= float64(time.Now().Unix()) {
		return "expired"
	}
	if expectedHost == "" {
		return "ok"
	}
	switch aud := claims["aud"].(type) {
	case string:
		if strings.EqualFold(tokenHost(aud), expectedHost) {
			return "ok"
		}
	case []any:
		for _, value := range aud {
			if audience, ok := value.(string); ok && strings.EqualFold(tokenHost(audience), expectedHost) {
				return "ok"
			}
		}
	}
	return "audience_mismatch"
}

func tokenHost(value string) string {
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "//" + value
	}
	u, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
