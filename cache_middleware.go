package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Cache TTL hints for MCP 2026-07-28 (SEP-2549). The tool surface and
// reference resources are fixed for the lifetime of the process (toolsets are
// selected at startup; reference markdown is embedded). Clients that honor
// ttlMs can skip re-fetching these on every session.
const (
	cacheScopePublic = "public"

	// tools/list: surface is static until process restart or redeploy.
	toolsListTTLMs = 10 * 60 * 1000 // 10 minutes

	// resources/list: four embedded reference URIs, never change at runtime.
	resourcesListTTLMs = 15 * 60 * 1000 // 15 minutes

	// resources/read: embedded markdown manuals; content is immutable at runtime.
	resourcesReadTTLMs = 60 * 60 * 1000 // 1 hour

	// server/discover: capabilities/versions stable for process lifetime.
	serverDiscoverTTLMs = 10 * 60 * 1000 // 10 minutes
)

// cacheTTLMiddleware sets ttlMs/cacheScope on list/read/discover results when
// the SDK left them at zero (no hint). Does not shorten TTLs already set.
func cacheTTLMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		if err != nil || result == nil {
			return result, err
		}
		applyCacheTTL(method, result)
		return result, err
	}
}

func applyCacheTTL(method string, result mcp.Result) {
	switch method {
	case "tools/list":
		if r, ok := result.(*mcp.ListToolsResult); ok {
			setCacheableIfUnset(&r.Cacheable, toolsListTTLMs)
		}
	case "resources/list":
		if r, ok := result.(*mcp.ListResourcesResult); ok {
			setCacheableIfUnset(&r.Cacheable, resourcesListTTLMs)
		}
	case "resources/read":
		if r, ok := result.(*mcp.ReadResourceResult); ok {
			setCacheableIfUnset(&r.Cacheable, resourcesReadTTLMs)
		}
	case "server/discover":
		if r, ok := result.(*mcp.DiscoverResult); ok {
			setCacheableIfUnset(&r.Cacheable, serverDiscoverTTLMs)
		}
	}
}

func setCacheableIfUnset(c *mcp.Cacheable, ttlMs int) {
	if c.TTLMs != 0 {
		return
	}
	c.TTLMs = ttlMs
	if c.CacheScope == "" {
		c.CacheScope = cacheScopePublic
	}
}
