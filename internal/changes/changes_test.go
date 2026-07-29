package changes

import (
	"context"
	"io"
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

func TestGetChangesRequiresAbsoluteRangeAndScope(t *testing.T) {
	tests := []struct {
		name string
		args GetChangesArgs
		want string
	}{
		{name: "missing start", args: GetChangesArgs{EndTime: "2026-07-29T11:00:00Z", Service: "checkout-api"}, want: "start_time is required"},
		{name: "missing end", args: GetChangesArgs{StartTime: "2026-07-29T10:00:00Z", Service: "checkout-api"}, want: "end_time is required"},
		{name: "missing scope", args: GetChangesArgs{StartTime: "2026-07-29T10:00:00Z", EndTime: "2026-07-29T11:00:00Z"}, want: "at least one scope"},
		{name: "invalid range", args: GetChangesArgs{StartTime: "2026-07-29T11:00:00Z", EndTime: "2026-07-29T10:00:00Z", Service: "checkout-api"}, want: "after start_time"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := NewGetChangesHandler(http.DefaultClient, changesTestConfig("unused"))(
				context.Background(), &mcp.CallToolRequest{}, tt.args,
			)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestGetChangesForwardsFederationContract(t *testing.T) {
	const upstream = "{\n  \"unknown\":\"preserved\",\n  \"changes\":[{\"id\":\"change:1\",\"occurred_at_unix_nano\":1753785000000000000}]\n}"
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Clone(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstream)
	}))
	defer server.Close()

	result, _, err := NewGetChangesHandler(server.Client(), changesTestConfig(server.URL))(
		context.Background(), &mcp.CallToolRequest{}, GetChangesArgs{
			StartTime:    " 2026-07-29T10:00:00Z ",
			EndTime:      " 2026-07-29T11:00:00Z ",
			Service:      " checkout-api ",
			Environment:  " production ",
			Cluster:      " cluster-a ",
			Namespace:    " payments ",
			ResourceKind: " Deployment ",
			ResourceName: " checkout-api ",
			ResourceUID:  " resource-uid ",
			Sources:      []string{"change_events", "kubernetes_events"},
			Categories:   []string{"deployment", "scaling"},
			Order:        "asc",
			Cursor:       "next-page",
			Limit:        25,
		},
	)
	if err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if captured == nil {
		t.Fatal("changes endpoint was not called")
	}
	if captured.Method != http.MethodGet || captured.URL.Path != "/changes" {
		t.Fatalf("request = %s %s, want GET /changes", captured.Method, captured.URL.Path)
	}
	for name, want := range map[string]string{
		constants.HeaderAccept:         constants.HeaderAcceptJSON,
		constants.HeaderUserAgent:      constants.UserAgentLast9MCP,
		constants.HeaderXLast9APIToken: constants.BearerPrefix + "test-token",
	} {
		if got := captured.Header.Get(name); got != want {
			t.Errorf("header[%s] = %q, want %q", name, got, want)
		}
	}
	query := captured.URL.Query()
	for name, want := range map[string]string{
		"start_time": "2026-07-29T10:00:00Z", "end_time": "2026-07-29T11:00:00Z",
		"service": "checkout-api", "environment": "production", "cluster": "cluster-a",
		"namespace": "payments", "resource_kind": "Deployment", "resource_name": "checkout-api",
		"resource_uid": "resource-uid", "order": "asc", "cursor": "next-page", "limit": "25",
		"region": "ap-south-1", "data_source_name": "primary",
	} {
		if got := query.Get(name); got != want {
			t.Errorf("query[%s] = %q, want %q", name, got, want)
		}
	}
	if got := query["sources"]; len(got) != 2 || got[0] != "change_events" || got[1] != "kubernetes_events" {
		t.Errorf("sources = %#v", got)
	}
	if got := query["categories"]; len(got) != 2 || got[0] != "deployment" || got[1] != "scaling" {
		t.Errorf("categories = %#v", got)
	}
	output := utils.GetTextContent(t, result)
	if output != upstream {
		t.Fatalf("API response bytes changed:\ngot:  %q\nwant: %q", output, upstream)
	}
}

func TestGetChangesRejectsInvalidOptionalControls(t *testing.T) {
	base := GetChangesArgs{
		StartTime: "2026-07-29T10:00:00Z",
		EndTime:   "2026-07-29T11:00:00Z",
		Cluster:   "cluster-a",
	}
	tests := []struct {
		name string
		edit func(*GetChangesArgs)
		want string
	}{
		{name: "order", edit: func(args *GetChangesArgs) { args.Order = "newest" }, want: "order must be"},
		{name: "limit", edit: func(args *GetChangesArgs) { args.Limit = -1 }, want: "limit must be positive"},
		{name: "source", edit: func(args *GetChangesArgs) { args.Sources = []string{"alerts"} }, want: "unsupported source"},
		{name: "category", edit: func(args *GetChangesArgs) { args.Categories = []string{"health"} }, want: "unsupported category"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := base
			tt.edit(&args)
			_, _, err := NewGetChangesHandler(http.DefaultClient, changesTestConfig("unused"))(
				context.Background(), &mcp.CallToolRequest{}, args,
			)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestGetChangesExplainsInvalidCursorRecovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "internal validation details")
	}))
	defer server.Close()

	_, _, err := NewGetChangesHandler(server.Client(), changesTestConfig(server.URL))(
		context.Background(), &mcp.CallToolRequest{}, GetChangesArgs{
			StartTime: "2026-07-29T10:00:00Z", EndTime: "2026-07-29T11:00:00Z",
			Namespace: "payments", Cursor: "invalid",
		},
	)

	if err == nil || !strings.Contains(err.Error(), "unchanged controls") {
		t.Fatalf("error = %v, want cursor recovery guidance", err)
	}
	if strings.Contains(err.Error(), "internal validation details") {
		t.Fatalf("error leaked upstream response: %v", err)
	}
}

func TestGetChangesSanitizesUpstreamErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "secret upstream response")
	}))
	defer server.Close()

	_, _, err := NewGetChangesHandler(server.Client(), changesTestConfig(server.URL))(
		context.Background(), &mcp.CallToolRequest{}, GetChangesArgs{
			StartTime: "2026-07-29T10:00:00Z",
			EndTime:   "2026-07-29T11:00:00Z",
			Namespace: "payments",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("error = %v, want sanitized status", err)
	}
	if strings.Contains(err.Error(), "secret upstream response") {
		t.Fatalf("error leaked upstream response: %v", err)
	}
}

func changesTestConfig(apiBaseURL string) models.Config {
	return models.Config{
		APIBaseURL: apiBaseURL, Region: "ap-south-1", DatasourceName: "primary",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}
