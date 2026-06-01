package logs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testAttrConfig(apiBaseURL string) models.Config {
	return models.Config{
		APIBaseURL: apiBaseURL,
		Region:     "us-east-1",
		OrgSlug:    "last9",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

// TestFetchLogAttributeNames_UsesLabelsEndpoint verifies that FetchLogAttributeNames
// uses the GET /v1/labels endpoint, which returns the full label set. The
// POST /v2/series/json empty-pipeline path returns only a subset and must not be used.
func TestFetchLogAttributeNames_UsesLabelsEndpoint(t *testing.T) {
	var capturedPath, capturedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":["ServiceName","severity","hook_event","http.status_code"]}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	attrs, err := FetchLogAttributeNames(context.Background(), server.Client(), cfg)
	if err != nil {
		t.Fatalf("FetchLogAttributeNames returned error: %v", err)
	}

	// Must use GET /v1/labels, never POST /v2/series/json
	if !strings.Contains(capturedPath, "/v1/labels") || capturedMethod != "GET" {
		t.Errorf("expected GET /v1/labels endpoint, got: %s %s", capturedMethod, capturedPath)
	}
	if strings.Contains(capturedPath, "series/json") {
		t.Errorf("must not use series/json endpoint, got: %s", capturedPath)
	}

	if len(attrs) == 0 {
		t.Error("expected non-empty attribute list")
	}
}

// TestGetLogAttributesHandler_CapsTimeRangeAt1Hour verifies that when the caller
// requests a window longer than 1 hour, the handler caps the API request to 1 hour.
func TestGetLogAttributesHandler_CapsTimeRangeAt1Hour(t *testing.T) {
	var capturedStart, capturedEnd int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		end, _ := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
		capturedStart = start
		capturedEnd = end

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":["ServiceName","severity"]}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesHandler(server.Client(), cfg)

	// Request a 3-hour lookback — handler should cap to 1 hour internally.
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesArgs{
		LookbackMinutes: 180,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if capturedStart == 0 || capturedEnd == 0 {
		t.Fatal("no request captured — handler may have returned early")
	}

	windowSeconds := capturedEnd - capturedStart
	maxAllowed := int64(utils.MaxLogAttributeLookbackMinutes * 60)
	if windowSeconds > maxAllowed {
		t.Errorf("API request window %ds exceeds %ds cap; large windows are not capped", windowSeconds, maxAllowed)
	}
}

// TestGetLogAttributesHandler_ShortWindowStillUsesLabels verifies that a short
// explicit lookback (well under the legacy 20-minute threshold) still routes to
// GET /v1/labels — not the inferior POST /v2/series/json empty-pipeline path that
// returns only a subset of attributes.
func TestGetLogAttributesHandler_ShortWindowStillUsesLabels(t *testing.T) {
	var capturedPath, capturedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":["ServiceName","severity","hook_event"]}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	handler := NewGetLogAttributesHandler(server.Client(), cfg)

	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetLogAttributesArgs{
		LookbackMinutes: 5,
	})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if capturedMethod != "GET" || !strings.Contains(capturedPath, "/v1/labels") {
		t.Errorf("short window must still use GET /v1/labels, got: %s %s", capturedMethod, capturedPath)
	}
	if strings.Contains(capturedPath, "series/json") {
		t.Errorf("short window must not fall back to series/json, got: %s", capturedPath)
	}
}

// TestFetchLogAttributeNames_ReturnsAllLabelsFromAPI verifies that attributes
// returned by the /v1/labels API are surfaced in the result slice.
func TestFetchLogAttributeNames_ReturnsAllLabelsFromAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":["ServiceName","severity","hook_event","http.status_code","method"]}`))
	}))
	defer server.Close()

	cfg := testAttrConfig(server.URL)
	attrs, err := FetchLogAttributeNames(context.Background(), server.Client(), cfg)
	if err != nil {
		t.Fatalf("FetchLogAttributeNames returned error: %v", err)
	}

	attrSet := make(map[string]bool, len(attrs))
	for _, a := range attrs {
		attrSet[a] = true
	}

	for _, expected := range []string{"ServiceName", "severity", "hook_event", "http.status_code", "method"} {
		if !attrSet[expected] {
			t.Errorf("expected attribute %q in results, got: %v", expected, attrs)
		}
	}
}
