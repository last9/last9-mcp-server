package feedback

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	apifeedback "last9-mcp/internal/last9api/feedback"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testConfig(baseURL string) models.Config {
	return models.Config{
		APIBaseURL:    baseURL,
		ServerVersion: "1.2.3",
		TokenManager:  &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)},
	}
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) != 1 {
		t.Fatalf("result = %+v, want one content item", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content = %T, want *mcp.TextContent", result.Content[0])
	}
	return text.Text
}

func TestSendFeedbackSendsNormalizedReportWithServerVersion(t *testing.T) {
	var got apifeedback.SendMCPFeedbackInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"feedback_id":"fb-1"}`))
	}))
	defer srv.Close()

	handler := NewSendFeedbackHandler(srv.Client(), testConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SendFeedbackArgs{
		Category: " Wrong_Result ",
		Summary:  "  get_service_summary reported 0 rps for a busy service ",
		ToolName: "get_service_summary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result is an error: %s", resultText(t, result))
	}
	want := apifeedback.SendMCPFeedbackInput{
		Category:      "wrong_result",
		Summary:       "get_service_summary reported 0 rps for a busy service",
		ToolName:      "get_service_summary",
		ServerVersion: "1.2.3",
	}
	if got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}
	var out sendFeedbackResult
	if err := json.Unmarshal([]byte(resultText(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	if out.FeedbackID != "fb-1" {
		t.Errorf("feedback_id = %q, want fb-1", out.FeedbackID)
	}
}

func TestSendFeedbackRejectsInvalidArgsWithoutCallingAPI(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	handler := NewSendFeedbackHandler(srv.Client(), testConfig(srv.URL))

	cases := map[string]struct {
		args SendFeedbackArgs
		want string
	}{
		"unknown category": {SendFeedbackArgs{Category: "rant", Summary: "x"}, "category must be one of"},
		"empty summary":    {SendFeedbackArgs{Category: "bug", Summary: "  "}, "summary is required"},
		"long summary":     {SendFeedbackArgs{Category: "bug", Summary: strings.Repeat("é", maxSummaryRunes+1)}, "summary exceeds 300"},
		"long details":     {SendFeedbackArgs{Category: "bug", Summary: "x", Details: strings.Repeat("a", maxDetailsRunes+1)}, "details exceeds 4000"},
		"long tool name":   {SendFeedbackArgs{Category: "bug", Summary: "x", ToolName: strings.Repeat("a", maxToolNameRunes+1)}, "tool_name exceeds 100"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.Contains(resultText(t, result), tc.want) {
				t.Errorf("result = %q (isError %v), want error containing %q", resultText(t, result), result.IsError, tc.want)
			}
		})
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("API calls = %d, want 0", n)
	}
}

func TestSendFeedbackReturnsAPIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
	}))
	defer srv.Close()

	handler := NewSendFeedbackHandler(srv.Client(), testConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, SendFeedbackArgs{Category: "bug", Summary: "x"})
	if err == nil {
		t.Fatal("err = nil, want the API's 429")
	}
}
