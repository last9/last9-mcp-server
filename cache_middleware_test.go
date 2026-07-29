package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestApplyCacheTTL_ToolsList(t *testing.T) {
	result := &mcp.ListToolsResult{}
	applyCacheTTL("tools/list", result)
	if result.TTLMs != toolsListTTLMs {
		t.Fatalf("TTLMs = %d, want %d", result.TTLMs, toolsListTTLMs)
	}
	if result.CacheScope != cacheScopePublic {
		t.Fatalf("CacheScope = %q, want %q", result.CacheScope, cacheScopePublic)
	}
}

func TestApplyCacheTTL_ResourcesRead(t *testing.T) {
	result := &mcp.ReadResourceResult{}
	applyCacheTTL("resources/read", result)
	if result.TTLMs != resourcesReadTTLMs {
		t.Fatalf("TTLMs = %d, want %d", result.TTLMs, resourcesReadTTLMs)
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
}
