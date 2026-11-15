package remapping

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

func testRemappingConfig(actionURL string) models.Config {
	return models.Config{
		APIBaseURL: actionURL,
		ActionURL:  actionURL,
		Region:     "us-east-1",
		OrgSlug:    "test-org",
		ClusterID:  "cluster-1",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func TestGetRemappingRulesHandlerRejectsBeforeAnyRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for invalid input")
	}))
	defer server.Close()

	noRegion := testRemappingConfig(server.URL)
	noRegion.Region = ""
	tests := []struct {
		name    string
		cfg     models.Config
		args    GetRemappingRulesArgs
		wantErr string
	}{
		{"unknown rule type", testRemappingConfig(server.URL), GetRemappingRulesArgs{RuleType: "bogus"}, `invalid rule_type "bogus"`},
		{"no region anywhere", noRegion, GetRemappingRulesArgs{RuleType: ruleTypeLogsMap}, "region is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewGetRemappingRulesHandler(server.Client(), tc.cfg)
			_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestGetRemappingRulesHandlerContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	defer close(blocked)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	handler := NewGetRemappingRulesHandler(server.Client(), testRemappingConfig(server.URL))
	_, _, err := handler(ctx, &mcp.CallToolRequest{}, GetRemappingRulesArgs{RuleType: ruleTypeLogsExtract})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestGetRemappingRulesHandlerRequiresRuleType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called for invalid input")
	}))
	defer server.Close()

	handler := NewGetRemappingRulesHandler(server.Client(), testRemappingConfig(server.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetRemappingRulesArgs{})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestGetRemappingRulesHandlerSendsGETWithRegion(t *testing.T) {
	var capturedMethod, capturedRegion string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedRegion = r.URL.Query().Get("region")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]string{{"id": "rule-1", "name": "test"}})
	}))
	defer server.Close()

	handler := NewGetRemappingRulesHandler(server.Client(), testRemappingConfig(server.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetRemappingRulesArgs{
		RuleType: ruleTypeLogsMap,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if capturedMethod != http.MethodGet {
		t.Fatalf("expected GET, got %s", capturedMethod)
	}
	if capturedRegion != "us-east-1" {
		t.Fatalf("expected region=us-east-1, got %q", capturedRegion)
	}
}
