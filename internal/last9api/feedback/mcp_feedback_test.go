package feedback

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
)

func TestSendMCPFeedbackPostsInputAndReturnsID(t *testing.T) {
	var got SendMCPFeedbackInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/mcp_feedback" {
			t.Errorf("request = %s %s, want POST /mcp_feedback", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"feedback_id":"fb-1"}`))
	}))
	defer srv.Close()

	client := last9api.NewClient(srv.Client(), models.Config{
		APIBaseURL:   srv.URL,
		TokenManager: &auth.TokenManager{AccessToken: "token", ExpiresAt: time.Now().Add(time.Hour)},
	})
	in := SendMCPFeedbackInput{Category: "bug", Summary: "get_logs ignored the filter", ToolName: "get_logs"}

	out, err := SendMCPFeedback(context.Background(), client, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.FeedbackID != "fb-1" {
		t.Errorf("feedback_id = %q, want fb-1", out.FeedbackID)
	}
	if got != in {
		t.Errorf("body = %+v, want %+v", got, in)
	}
}
