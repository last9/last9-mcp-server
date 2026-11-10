package apm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestPromqlLabelValuesHandler_UsesLookbackAndExplicitPrecedence(t *testing.T) {
	type capturedReq struct {
		Label     string   `json:"label"`
		Matches   []string `json:"matches"`
		Timestamp int64    `json:"timestamp"`
		Window    int64    `json:"window"`
	}

	var captured []capturedReq
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/prom_label_values") {
			t.Fatalf("expected prom_label_values endpoint, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var reqPayload capturedReq
		_ = json.Unmarshal(body, &reqPayload)
		captured = append(captured, reqPayload)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["bucket-a"]`))
	}))
	defer server.Close()

	cfg := models.Config{
		APIBaseURL: server.URL,
		Region:     "us-east-1",
	}
	cfg.TokenManager = &auth.TokenManager{
		AccessToken: "mock-access-token",
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
	}

	handler := NewPromqlLabelValuesHandler(server.Client(), cfg)

	// Default omitted lookback must stay 60 minutes (no regression).
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, PromqlLabelValuesArgs{
		MatchQuery: `aws_s3_bucketsizebytes{Namespace="AWS/S3"}`,
		Label:      "BucketName",
	})
	if err != nil {
		t.Fatalf("handler returned error for default lookback: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(captured))
	}
	if captured[0].Window != 3600 {
		t.Fatalf("default window = %d, want %d", captured[0].Window, int64(3600))
	}
	if captured[0].Label != "BucketName" {
		t.Fatalf("label = %q, want BucketName", captured[0].Label)
	}

	// Wide lookback (4d) must widen the upstream window so daily/sparse series can surface.
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlLabelValuesArgs{
		MatchQuery:      `aws_s3_bucketsizebytes{Namespace="AWS/S3"}`,
		Label:           "BucketName",
		LookbackMinutes: 5760,
	})
	if err != nil {
		t.Fatalf("handler returned error for 4d lookback: %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("expected 2 captured requests, got %d", len(captured))
	}
	if captured[1].Window != 5760*60 {
		t.Fatalf("4d window = %d, want %d", captured[1].Window, int64(5760*60))
	}

	// Explicit ISO bounds override lookback_minutes.
	_, _, err = handler(context.Background(), &mcp.CallToolRequest{}, PromqlLabelValuesArgs{
		MatchQuery:      `aws_s3_bucketsizebytes{Namespace="AWS/S3"}`,
		Label:           "BucketName",
		StartTimeISO:    "2025-06-23T16:00:00Z",
		EndTimeISO:      "2025-06-23T16:10:00Z",
		LookbackMinutes: 5760,
	})
	if err != nil {
		t.Fatalf("handler returned error for explicit mode: %v", err)
	}
	if len(captured) != 3 {
		t.Fatalf("expected 3 captured requests, got %d", len(captured))
	}
	if captured[2].Window != 600 {
		t.Fatalf("window with explicit timestamps = %d, want %d", captured[2].Window, int64(600))
	}
	if captured[2].Timestamp != time.Date(2025, 6, 23, 16, 10, 0, 0, time.UTC).Unix() {
		t.Fatalf("timestamp = %d, want end unix", captured[2].Timestamp)
	}
}

func TestPromqlLabelValuesHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewPromqlLabelValuesHandler(http.DefaultClient, *cfg)

	args := PromqlLabelValuesArgs{
		MatchQuery:   "up",
		Label:        "job",
		StartTimeISO: time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339),
		EndTimeISO:   time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	req := &mcp.CallToolRequest{}
	result, _, err := handler(ctx, req, args)

	if utils.CheckAPIError(t, err) {
		return
	}

	text := utils.GetTextContent(t, result)

	var labelValues []string
	if err := json.Unmarshal([]byte(text), &labelValues); err != nil {
		t.Logf("Integration test successful. Response is formatted text (not JSON)")
	} else {
		t.Logf("Integration test successful: found %d label value(s) for label '%s'", len(labelValues), args.Label)
	}
}
