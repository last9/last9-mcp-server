package suggest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDidYouMeanHandler_Suggestions(t *testing.T) {
	tests := []struct {
		name            string
		args            DidYouMeanArgs
		apiSuggestions  []suggestionItem
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "typo returns top suggestions with scores",
			args: DidYouMeanArgs{Query: "paymnt-svc"},
			apiSuggestions: []suggestionItem{
				{Name: "payment-service", Type: "service", Score: 0.92},
				{Name: "payment-gateway", Type: "service", Score: 0.78},
			},
			wantContains: []string{
				"paymnt-svc",
				"payment-service", "92%",
				"payment-gateway", "78%",
				"type: service",
			},
		},
		{
			name:           "no suggestions returns helpful message",
			args:           DidYouMeanArgs{Query: "xyzunknown123"},
			apiSuggestions: []suggestionItem{},
			wantContains:   []string{"No suggestions found", "xyzunknown123"},
		},
		{
			name: "type filter reflected in output",
			args: DidYouMeanArgs{Query: "prod", Type: "environment"},
			apiSuggestions: []suggestionItem{
				{Name: "production", Type: "environment", Score: 0.89},
			},
			wantContains: []string{"production", "89%", "environment"},
		},
		{
			name: "multiple suggestions ranked by score",
			args: DidYouMeanArgs{Query: "auth"},
			apiSuggestions: []suggestionItem{
				{Name: "auth-service", Type: "service", Score: 0.95},
				{Name: "authentication-api", Type: "service", Score: 0.81},
				{Name: "auth-db", Type: "service", Score: 0.76},
			},
			wantContains: []string{"auth-service", "95%", "authentication-api", "81%", "auth-db", "76%"},
		},
		{
			name: "more than three suggestions are truncated",
			args: DidYouMeanArgs{Query: "auth"},
			apiSuggestions: []suggestionItem{
				{Name: "auth-service", Type: "service", Score: 0.95},
				{Name: "authentication-api", Type: "service", Score: 0.81},
				{Name: "auth-db", Type: "service", Score: 0.76},
				{Name: "auth-cache", Type: "service", Score: 0.74},
			},
			wantContains:    []string{"auth-service", "authentication-api", "auth-db"},
			wantNotContains: []string{"auth-cache"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, _, err := executeDidYouMean(t, tt.apiSuggestions, http.StatusOK, tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(text, want) {
					t.Errorf("response missing %q\ngot:\n%s", want, text)
				}
			}
			for _, unwanted := range tt.wantNotContains {
				if strings.Contains(text, unwanted) {
					t.Errorf("response unexpectedly contained %q\ngot:\n%s", unwanted, text)
				}
			}
		})
	}
}

func TestDidYouMeanHandler_EmptyQuery(t *testing.T) {
	_, _, err := executeDidYouMean(t, nil, http.StatusOK, DidYouMeanArgs{Query: ""})
	if err == nil {
		t.Fatal("expected error for empty query, got nil")
	}
	if !strings.Contains(err.Error(), "query parameter is required") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDidYouMeanHandler_APIError(t *testing.T) {
	_, _, err := executeDidYouMean(t, nil, http.StatusInternalServerError, DidYouMeanArgs{Query: "prod"})
	if err == nil {
		t.Fatal("expected error on API 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected HTTP status in error message, got: %v", err)
	}
}

func TestDidYouMeanHandler_APIErrorTruncatesBody(t *testing.T) {
	const hiddenTail = "SHOULD_NOT_APPEAR"
	body := strings.Repeat("a", maxSuggestErrorBodyBytes) + hiddenTail

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertSuggestRequest(t, w, r) {
			return
		}
		http.Error(w, body, http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewDidYouMeanHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DidYouMeanArgs{Query: "prod"})
	if err == nil {
		t.Fatal("expected error on API 500, got nil")
	}
	if strings.Contains(err.Error(), hiddenTail) {
		t.Fatalf("expected error body to be truncated, got %q", err)
	}
}

func TestDidYouMeanHandler_MissingTokenManager(t *testing.T) {
	cfg := models.Config{APIBaseURL: "https://example.com"}

	handler := NewDidYouMeanHandler(http.DefaultClient, cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DidYouMeanArgs{Query: "prod"})
	if err == nil {
		t.Fatal("expected error when token manager is missing")
	}
	if !strings.Contains(err.Error(), "token manager is not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDidYouMeanHandler_TypeFilterForwardedToAPI(t *testing.T) {
	var capturedType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertSuggestRequest(t, w, r) {
			return
		}
		var body suggestRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		capturedType = body.Type
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(suggestAPIResponse{
			Query:       body.Query,
			Suggestions: []suggestionItem{{Name: "production", Type: "environment", Score: 0.9}},
		})
	}))
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewDidYouMeanHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DidYouMeanArgs{
		Query: "prod",
		Type:  "environment",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedType != "environment" {
		t.Errorf("expected type=environment forwarded to API, got %q", capturedType)
	}
}

func TestDidYouMeanHandler_PromCredentialsForwardedToAPI(t *testing.T) {
	var capturedReadURL, capturedUsername, capturedPassword string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertSuggestRequest(t, w, r) {
			return
		}
		var body suggestRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		capturedReadURL = body.ReadURL
		capturedUsername = body.Username
		capturedPassword = body.Password
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(suggestAPIResponse{
			Query:       body.Query,
			Suggestions: []suggestionItem{},
		})
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL:         server.URL,
		PrometheusReadURL:  "https://prom.example.com",
		PrometheusUsername: "user",
		PrometheusPassword: "pass",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewDidYouMeanHandler(server.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, DidYouMeanArgs{Query: "host1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReadURL != "https://prom.example.com" {
		t.Errorf("expected read_url forwarded, got %q", capturedReadURL)
	}
	if capturedUsername != "user" {
		t.Errorf("expected username forwarded, got %q", capturedUsername)
	}
	if capturedPassword != "pass" {
		t.Errorf("expected password forwarded, got %q", capturedPassword)
	}
}

// executeDidYouMean is a test helper that spins up a mock API server, runs the
// did_you_mean handler against it, and returns the text content of the result.
func executeDidYouMean(
	t *testing.T,
	suggestions []suggestionItem,
	statusCode int,
	args DidYouMeanArgs,
) (string, *mcp.CallToolResult, error) {
	t.Helper()

	server := newSuggestTestServer(t, suggestions, statusCode)
	defer server.Close()

	cfg := models.Config{APIBaseURL: server.URL}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	handler := NewDidYouMeanHandler(server.Client(), cfg)
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return "", result, err
	}

	return utils.GetTextContent(t, result), result, nil
}

func assertSuggestRequest(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()

	if r.URL.Path != constants.EndpointSuggest {
		t.Errorf("unexpected API path: got %q, want %q", r.URL.Path, constants.EndpointSuggest)
		http.Error(w, "unexpected path", http.StatusNotFound)
		return false
	}
	if r.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", r.Method)
		http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		return false
	}

	return true
}

func newSuggestTestServer(
	t *testing.T,
	suggestions []suggestionItem,
	statusCode int,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertSuggestRequest(t, w, r) {
			return
		}
		var body suggestRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if statusCode == http.StatusOK {
			_ = json.NewEncoder(w).Encode(suggestAPIResponse{
				Query:       body.Query,
				Suggestions: suggestions,
			})
		}
	}))
}
