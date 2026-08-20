package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestApplyCacheTTL_SetsHints(t *testing.T) {
	cases := []struct {
		method string
		result mcp.Result
		want   int
	}{
		{"tools/list", &mcp.ListToolsResult{}, toolsListTTLMs},
		{"prompts/list", &mcp.ListPromptsResult{}, promptsListTTLMs},
		{"resources/list", &mcp.ListResourcesResult{}, resourcesListTTLMs},
		{"resources/templates/list", &mcp.ListResourceTemplatesResult{}, resourceTemplatesListTTLMs},
		{"resources/read", &mcp.ReadResourceResult{}, resourcesReadTTLMs},
		{"server/discover", &mcp.DiscoverResult{}, serverDiscoverTTLMs},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			applyCacheTTL(tc.method, tc.result)
			got, ok := tc.result.(mcp.CacheableResult)
			if !ok {
				t.Fatalf("result type %T is not CacheableResult", tc.result)
			}
			if got.GetTTLMs() != tc.want {
				t.Fatalf("TTLMs = %d, want %d", got.GetTTLMs(), tc.want)
			}
			if got.GetCacheScope() != cacheScopePublic {
				t.Fatalf("CacheScope = %q, want %q", got.GetCacheScope(), cacheScopePublic)
			}
		})
	}
}

func TestApplyCacheTTL_DoesNotOverrideExisting(t *testing.T) {
	result := &mcp.ListToolsResult{
		Cacheable: mcp.Cacheable{TTLMs: 42, CacheScope: "private"},
	}
	applyCacheTTL("tools/list", result)
	if result.TTLMs != 42 {
		t.Fatalf("TTLMs = %d, want 42 (must not override)", result.TTLMs)
	}
	if result.CacheScope != "private" {
		t.Fatalf("CacheScope = %q, want private", result.CacheScope)
	}
}

func TestApplyCacheTTL_PreservesExistingScopeWhenTTLUnset(t *testing.T) {
	result := &mcp.ListToolsResult{
		Cacheable: mcp.Cacheable{CacheScope: "private"},
	}
	applyCacheTTL("tools/list", result)
	if result.TTLMs != toolsListTTLMs {
		t.Fatalf("TTLMs = %d, want %d", result.TTLMs, toolsListTTLMs)
	}
	if result.CacheScope != "private" {
		t.Fatalf("CacheScope = %q, want private", result.CacheScope)
	}
}

func TestCacheTTLMiddleware(t *testing.T) {
	handler := cacheTTLMiddleware(func(_ context.Context, method string, _ mcp.Request) (mcp.Result, error) {
		if method != "tools/list" {
			t.Fatalf("method = %q, want tools/list", method)
		}
		return &mcp.ListToolsResult{}, nil
	})

	result, err := handler(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	r, ok := result.(*mcp.ListToolsResult)
	if !ok {
		t.Fatalf("result type %T, want *mcp.ListToolsResult", result)
	}
	if r.TTLMs != toolsListTTLMs {
		t.Fatalf("TTLMs = %d, want %d", r.TTLMs, toolsListTTLMs)
	}
	if r.CacheScope != cacheScopePublic {
		t.Fatalf("CacheScope = %q, want %q", r.CacheScope, cacheScopePublic)
	}
}

func TestCacheTTLMiddleware_ErrorPassthrough(t *testing.T) {
	expectedErr := fmt.Errorf("handler error")
	handler := cacheTTLMiddleware(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return nil, expectedErr
	})
	result, err := handler(context.Background(), "tools/list", nil)
	if err != expectedErr {
		t.Fatalf("err = %v, want %v", err, expectedErr)
	}
	if result != nil {
		t.Fatalf("result = %v, want nil", result)
	}
}

func TestCacheTTLMiddleware_NilResultPassthrough(t *testing.T) {
	handler := cacheTTLMiddleware(func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return nil, nil
	})
	result, err := handler(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if result != nil {
		t.Fatalf("result = %v, want nil", result)
	}
}

func TestCacheTTLMiddleware_LiveServerListResults(t *testing.T) {
	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	registerReferenceResources(server)
	server.Server.AddReceivingMiddleware(cacheTTLMiddleware)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "cache-ttl-test", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if tools.TTLMs != toolsListTTLMs {
		t.Fatalf("tools/list TTLMs = %d, want %d", tools.TTLMs, toolsListTTLMs)
	}

	prompts, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if prompts.TTLMs != promptsListTTLMs {
		t.Fatalf("prompts/list TTLMs = %d, want %d", prompts.TTLMs, promptsListTTLMs)
	}

	resources, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if resources.TTLMs != resourcesListTTLMs {
		t.Fatalf("resources/list TTLMs = %d, want %d", resources.TTLMs, resourcesListTTLMs)
	}

	templates, err := session.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	if templates.TTLMs != resourceTemplatesListTTLMs {
		t.Fatalf("resources/templates/list TTLMs = %d, want %d", templates.TTLMs, resourceTemplatesListTTLMs)
	}

	read, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "last9://reference/logjson"})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if read.TTLMs != resourcesReadTTLMs {
		t.Fatalf("resources/read TTLMs = %d, want %d", read.TTLMs, resourcesReadTTLMs)
	}
}
