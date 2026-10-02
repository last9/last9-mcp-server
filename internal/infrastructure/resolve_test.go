package infrastructure

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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testResolveConfig(apiBase string) models.Config {
	return models.Config{
		APIBaseURL: apiBase,
		OrgSlug:    "test-org",
		Region:     "us-east-1",
		ClusterID:  "cluster-uuid",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(24 * time.Hour),
		},
	}
}

func TestGetInfrastructureContext_SendsRegionHeaderAndClusterID(t *testing.T) {
	var gotRegion, gotPath, gotAuth, gotQuery string
	var payload resolvePayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRegion = r.Header.Get("region")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("X-LAST9-API-TOKEN")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "matched",
			"anchor": map[string]any{
				"id": "host:i-abc",
				"ui": map[string]any{"href": "/v2/organizations/test-org/host?host=i-abc"},
			},
			"relationships": []any{},
		})
	}))
	defer srv.Close()

	handler := NewGetInfrastructureContextHandler(srv.Client(), testResolveConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
		EntityType: "host",
		Selectors:  InfrastructureSelectors{Instance: "10.0.0.1:9100"},
		Timestamp:  1700000000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/infrastructure/resolve" {
		t.Fatalf("path %q", gotPath)
	}
	if gotRegion != "us-east-1" {
		t.Fatalf("region header %q", gotRegion)
	}
	if gotQuery != "" {
		t.Fatalf("region must be a header, query was %q", gotQuery)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("auth %q", gotAuth)
	}
	if payload.ClusterID != "cluster-uuid" || payload.EntityType != "host" || payload.Timestamp != 1700000000 {
		t.Fatalf("payload %+v", payload)
	}
	if payload.Selectors["instance"] != "10.0.0.1:9100" {
		t.Fatalf("selectors %+v", payload.Selectors)
	}
	href, _ := result.Meta["reference_url"].(string)
	if href != "/v2/organizations/test-org/host?host=i-abc" {
		t.Fatalf("meta href %q", href)
	}
}

func TestGetInfrastructureContext_OverridesClusterAndRegion(t *testing.T) {
	var gotRegion string
	var payload resolvePayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRegion = r.Header.Get("region")
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"not_found","relationships":[]}`))
	}))
	defer srv.Close()

	handler := NewGetInfrastructureContextHandler(srv.Client(), testResolveConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
		EntityType: "k8s_node",
		Selectors:  InfrastructureSelectors{Cluster: "prod", Node: "ip-10-0-0-1"},
		ClusterID:  "other-cluster",
		Region:     "ap-south-1",
		Timestamp:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotRegion != "ap-south-1" {
		t.Fatalf("region %q", gotRegion)
	}
	if payload.ClusterID != "other-cluster" {
		t.Fatalf("cluster_id %q", payload.ClusterID)
	}
}

func TestGetInfrastructureContext_RejectsK8sCluster(t *testing.T) {
	handler := NewGetInfrastructureContextHandler(http.DefaultClient, testResolveConfig("http://example.invalid"))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
		EntityType: "k8s_cluster",
		Selectors:  InfrastructureSelectors{Cluster: "prod"},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported entity_type") {
		t.Fatalf("got %v", err)
	}
}

func TestGetInfrastructureContext_RequiresEntityType(t *testing.T) {
	handler := NewGetInfrastructureContextHandler(http.DefaultClient, testResolveConfig("http://example.invalid"))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{})
	if err == nil || !strings.Contains(err.Error(), "entity_type is required") {
		t.Fatalf("got %v", err)
	}
}

func TestGetInfrastructureContext_RequiresRegion(t *testing.T) {
	cfg := testResolveConfig("http://example.invalid")
	cfg.Region = ""
	handler := NewGetInfrastructureContextHandler(http.DefaultClient, cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
		EntityType: "host",
		Selectors:  InfrastructureSelectors{HostName: "web-01"},
	})
	if err == nil || !strings.Contains(err.Error(), "region is required") {
		t.Fatalf("got %v", err)
	}
}

func TestGetInfrastructureContext_RequiresClusterID(t *testing.T) {
	cfg := testResolveConfig("http://example.invalid")
	cfg.ClusterID = ""
	handler := NewGetInfrastructureContextHandler(http.DefaultClient, cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
		EntityType: "host",
		Selectors:  InfrastructureSelectors{HostName: "web-01"},
	})
	if err == nil || !strings.Contains(err.Error(), "cluster_id is required") {
		t.Fatalf("got %v", err)
	}
}

func TestGetInfrastructureContext_DoesNotSendPromCredentials(t *testing.T) {
	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		_, _ = w.Write([]byte(`{"status":"matched","relationships":[]}`))
	}))
	defer srv.Close()

	cfg := testResolveConfig(srv.URL)
	cfg.PrometheusReadURL = "https://prom.example/read"
	cfg.PrometheusUsername = "user"
	cfg.PrometheusPassword = "secret"
	handler := NewGetInfrastructureContextHandler(srv.Client(), cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
		EntityType: "host",
		Selectors:  InfrastructureSelectors{HostID: "i-abc"},
		Timestamp:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"read_url", "username", "password"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("must not send %s: %+v", key, raw)
		}
	}
}

func TestGetInfrastructureContext_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"cluster_id is required"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	handler := NewGetInfrastructureContextHandler(srv.Client(), testResolveConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
		EntityType: "host",
		Selectors:  InfrastructureSelectors{Instance: "a:1"},
		Timestamp:  1,
	})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("got %v", err)
	}
}

func TestGetInfrastructureContext_MissingTokenManager(t *testing.T) {
	cfg := testResolveConfig("http://example.invalid")
	cfg.TokenManager = nil
	handler := NewGetInfrastructureContextHandler(http.DefaultClient, cfg)
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
		EntityType: "host",
		Selectors:  InfrastructureSelectors{Instance: "a:1"},
		Timestamp:  1,
	})
	if err == nil || !strings.Contains(err.Error(), "token manager is not configured") {
		t.Fatalf("got %v", err)
	}
}

func TestGetInfrastructureContext_DefaultsTimestamp(t *testing.T) {
	var payload resolvePayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = w.Write([]byte(`{"status":"matched","relationships":[]}`))
	}))
	defer srv.Close()

	before := time.Now().Unix()
	handler := NewGetInfrastructureContextHandler(srv.Client(), testResolveConfig(srv.URL))
	_, _, err := handler(context.Background(), &mcp.CallToolRequest{}, GetInfrastructureContextArgs{
		EntityType: "k8s_pod",
		Selectors:  InfrastructureSelectors{Cluster: "prod", UID: "uid-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	after := time.Now().Unix()
	if payload.Timestamp < before || payload.Timestamp > after {
		t.Fatalf("timestamp %d not in [%d, %d]", payload.Timestamp, before, after)
	}
}
