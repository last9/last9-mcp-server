package profiles

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testCfg(baseURL string) models.Config {
	return models.Config{
		APIBaseURL: baseURL,
		Region:     "us-east-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}

func dataframeBody(metrics ...map[string]any) []byte {
	result := make([]any, 0, len(metrics))
	for _, m := range metrics {
		result = append(result, map[string]any{"metric": m})
	}
	body, _ := json.Marshal(map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": "dataframe",
			"result":     result,
		},
	})
	return body
}

func TestGetFlamegraphHandler(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(dataframeBody(
			map[string]any{
				"StackHash": "1",
				"Frames":    "leaf" + FrameDelimiter + "main",
				"samples":   float64(10),
			},
		))
	}))
	defer server.Close()

	handler := NewGetFlamegraphHandler(server.Client(), testCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetFlamegraphArgs{
		Service:         "api",
		Env:             "prod",
		LookbackMinutes: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(gotPath, "/profiles/api/v1/query_range/json?") {
		t.Fatalf("path=%s", gotPath)
	}
	if !strings.Contains(gotPath, "region=us-east-1") {
		t.Fatalf("missing region in %s", gotPath)
	}
	pipeline, _ := gotBody["pipeline"].([]any)
	if len(pipeline) < 1 {
		t.Fatalf("pipeline=%v", gotBody)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"name": "main"`) || !strings.Contains(text, `"total_samples": 10`) {
		t.Fatalf("response=%s", text)
	}
}

func TestGetTopFunctionsHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(dataframeBody(
			map[string]any{"StackHash": "1", "Frames": "hot" + FrameDelimiter + "main", "samples": float64(8)},
			map[string]any{"StackHash": "2", "Frames": "cool" + FrameDelimiter + "main", "samples": float64(2)},
		))
	}))
	defer server.Close()

	handler := NewGetTopFunctionsHandler(server.Client(), testCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetTopFunctionsArgs{
		Service: "api",
		Limit:   10,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"name": "hot"`) || !strings.Contains(text, `"self_samples": 8`) {
		t.Fatalf("response=%s", text)
	}
}

func TestGetProfileSummaryHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(dataframeBody(
			map[string]any{"StackHash": "1", "Frames": "a", "samples": float64(60)},
			map[string]any{"StackHash": "2", "Frames": "b", "samples": float64(40)},
		))
	}))
	defer server.Close()

	handler := NewGetProfileSummaryHandler(server.Client(), testCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetProfileSummaryArgs{
		Service: "api",
		TopN:    2,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "Top 2 CPU consumers for api are a, b") {
		t.Fatalf("response=%s", text)
	}
}

func TestGetProfileServicesHandler(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write(dataframeBody(
				map[string]any{"name": "svc-a", "samples": float64(90), "runtime": "go"},
				map[string]any{"name": "svc-b", "samples": float64(10), "runtime": "java"},
			))
			return
		}
		_, _ = w.Write(dataframeBody(
			map[string]any{"name": "svc-a", "last_profile": "2026-08-13T10:00:00Z"},
		))
	}))
	defer server.Close()

	handler := NewGetProfileServicesHandler(server.Client(), testCfg(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetProfileServicesArgs{})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"name": "svc-a"`) || !strings.Contains(text, `"count": 2`) {
		t.Fatalf("response=%s", text)
	}
	if calls != 2 {
		t.Fatalf("calls=%d want 2", calls)
	}
}

func TestGetFlamegraphRequiresService(t *testing.T) {
	handler := NewGetFlamegraphHandler(http.DefaultClient, testCfg("http://example.invalid"))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetFlamegraphArgs{})
	if err == nil || !strings.Contains(err.Error(), "service is required") {
		t.Fatalf("err=%v", err)
	}
}
