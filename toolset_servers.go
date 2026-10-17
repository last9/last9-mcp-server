package main

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"

	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

// toolsetServers caches one MCP server per distinct effective tool set, keyed
// by its sorted tool names; like the startup server, each is shared by every
// stateless request for that set. Parse accepts only named toolsets, so the
// cache is bounded by their combinations (135, under 1 MB each) without eviction.
type toolsetServers struct {
	build func(toolsets.Set) (*last9mcp.Last9MCPServer, error)

	mu      sync.Mutex
	cached  map[string]*toolsetServer
	servers []*last9mcp.Last9MCPServer
}

// toolsetServer is one cache entry; ready closes once handler or err is set,
// so concurrent misses for a set share its build.
type toolsetServer struct {
	ready   chan struct{}
	handler http.Handler
	err     error
}

// handler returns the handler for allowed, building its server on first use.
// Builds run outside mu so a slow build never stalls other toolsets.
func (c *toolsetServers) handler(allowed toolsets.Set) (http.Handler, error) {
	key := toolSetKey(allowed)
	c.mu.Lock()
	if entry, ok := c.cached[key]; ok {
		c.mu.Unlock()
		<-entry.ready
		return entry.handler, entry.err
	}
	entry := &toolsetServer{ready: make(chan struct{})}
	c.cached[key] = entry
	c.mu.Unlock()

	server, err := c.build(allowed)
	c.mu.Lock()
	if err != nil {
		// Drop the failed entry so the next request retries the build.
		delete(c.cached, key)
		entry.err = err
	} else {
		c.servers = append(c.servers, server)
		entry.handler = server.NewStreamableHTTPHandler(streamableOptions())
	}
	c.mu.Unlock()
	close(entry.ready)
	return entry.handler, entry.err
}

func (c *toolsetServers) shutdown(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var errs []error
	for _, server := range c.servers {
		errs = append(errs, server.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

func toolSetKey(allowed toolsets.Set) string {
	names := make([]string, 0, len(allowed))
	for name := range allowed {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}
