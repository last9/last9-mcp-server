package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Cache TTL hints for MCP 2026-07-28 (SEP-2549). The tool surface, prompts,
// and reference resources are fixed for the lifetime of the process (toolsets
// are selected at startup; workflow prompts and reference markdown are
// embedded). Clients that honor ttlMs can skip re-fetching these on every
// session. The upstream SDK emits ttlMs=0 (immediately stale) unless we set a
// hint here.
const (
	cacheScopePublic = "public"

	// tools/list and prompts/list: surface is static until process restart.
	toolsListTTLMs   = 10 * 60 * 1000 // 10 minutes
	promptsListTTLMs = 10 * 60 * 1000 // 10 minutes

	// resources/list and resources/templates/list: embedded reference URIs,
	// never change at runtime. Templates are currently empty but still a
	// spec-required cacheable list result.
	resourcesListTTLMs         = 15 * 60 * 1000 // 15 minutes
	resourceTemplatesListTTLMs = 15 * 60 * 1000 // 15 minutes

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
	case "prompts/list":
		if r, ok := result.(*mcp.ListPromptsResult); ok {
			setCacheableIfUnset(&r.Cacheable, promptsListTTLMs)
		}
	case "resources/list":
		if r, ok := result.(*mcp.ListResourcesResult); ok {
			setCacheableIfUnset(&r.Cacheable, resourcesListTTLMs)
		}
	case "resources/templates/list":
		if r, ok := result.(*mcp.ListResourceTemplatesResult); ok {
			setCacheableIfUnset(&r.Cacheable, resourceTemplatesListTTLMs)
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
