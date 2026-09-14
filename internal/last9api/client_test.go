package last9api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
)

func testCfg(baseURL string) models.Config {
	return models.Config{
		APIBaseURL: baseURL,
		Region:     "ap-south-1",
		OrgSlug:    "test-org",
		ClusterID:  "test-cluster",
		TokenManager: &auth.TokenManager{
			AccessToken: "mock-token",
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		},
	}
}

func TestDo_SetsHeadersAndEncodesBody(t *testing.T) {
	var gotPath, gotAuth, gotAccept, gotUA, gotCT string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-LAST9-API-TOKEN")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), testCfg(srv.URL))
	var out struct {
		OK bool `json:"ok"`
	}
	err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodPost, Path: "/thing", Body: map[string]any{"a": 1}}, &out)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotPath != "/thing" {
		t.Errorf("path = %q, want /thing", gotPath)
	}
	if gotAuth != "Bearer mock-token" {
		t.Errorf("auth = %q", gotAuth)
	}
	if gotAccept != "application/json" || gotCT != "application/json" {
		t.Errorf("accept = %q, content-type = %q", gotAccept, gotCT)
	}
	if gotUA == "" {
		t.Error("User-Agent not set")
	}
	if gotBody["a"] != float64(1) {
		t.Errorf("body = %v", gotBody)
	}
	if !out.OK {
		t.Error("response not decoded")
	}
}

func TestDo_PreservesTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), testCfg(srv.URL))
	if err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodGet, Path: "/things/"}, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotPath != "/things/" {
		t.Errorf("path = %q, want /things/ (trailing slash is significant)", gotPath)
	}
}

func TestDo_RegionHeaderAndQuery(t *testing.T) {
	var gotRegionHeader, gotRegionQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRegionHeader = r.Header.Get("region")
		gotRegionQuery = r.URL.Query().Get("region")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewClient(srv.Client(), testCfg(srv.URL))

	if err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodGet, Path: "/a"}, nil, WithRegionHeader()); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotRegionHeader != "ap-south-1" || gotRegionQuery != "" {
		t.Errorf("header = %q, query = %q; want header only", gotRegionHeader, gotRegionQuery)
	}

	gotRegionHeader, gotRegionQuery = "", ""
	if err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodGet, Path: "/a"}, nil,
		WithRegionQuery(), WithQuery(url.Values{"k": {"v"}})); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotRegionQuery != "ap-south-1" || gotRegionHeader != "" {
		t.Errorf("header = %q, query = %q; want query only", gotRegionHeader, gotRegionQuery)
	}
}

func TestDo_MissingTokenFailsBeforeRequest(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := testCfg(srv.URL)
	cfg.TokenManager = &auth.TokenManager{AccessToken: "", ExpiresAt: time.Now().Add(time.Hour)}
	c := NewClient(srv.Client(), cfg)

	err := c.Do(context.Background(), "test op", Request{Method: http.MethodGet, Path: "/a"}, nil)
	if err == nil {
		t.Fatal("want error for empty access token")
	}
	if called {
		t.Error("request was sent despite an empty access token")
	}
}

func TestDo_MissingRegionFailsWhenRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := testCfg(srv.URL)
	cfg.Region = ""
	c := NewClient(srv.Client(), cfg)

	if err := c.Do(context.Background(), "test op",
		Request{Method: http.MethodGet, Path: "/a"}, nil, WithRegionHeader()); err == nil {
		t.Fatal("want error when region is required but empty")
	}
}

func TestDo_MapsNon2xxToUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.Client(), testCfg(srv.URL))
	err := c.Do(context.Background(), "database query",
		Request{Method: http.MethodGet, Path: "/a"}, nil)
	if err == nil {
		t.Fatal("want error for HTTP 429")
	}
	if !strings.Contains(err.Error(), "database query") {
		t.Errorf("error does not name the operation: %v", err)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error does not carry the status: %v", err)
	}
}
