package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// toolsetsHeader narrows one request's tool surface; the query parameter
	// is for clients that cannot set headers. The header wins when both are sent.
	toolsetsHeader     = "X-Last9-Toolsets"
	toolsetsQueryParam = "toolsets"
	// maxToolsetServers bounds the cached per-toolset servers. A set past the
	// bound is served by a server built for that request alone.
	maxToolsetServers = 32
)

// HTTPServer wraps the MCP server for HTTP transport
type HTTPServer struct {
	server  *last9mcp.Last9MCPServer
	config  models.Config
	servers *toolsetServers
}

// NewHTTPServer creates a new HTTP-based MCP server. server serves the startup
// toolsets; build creates a server for a narrower set a request asks for.
func NewHTTPServer(server *last9mcp.Last9MCPServer, config models.Config, build func(toolsets.Set) (*last9mcp.Last9MCPServer, error)) *HTTPServer {
	return &HTTPServer{
		server: server,
		config: config,
		servers: &toolsetServers{
			build:     build,
			maxCached: maxToolsetServers,
			cached:    map[string]http.Handler{},
		},
	}
}

// Handler routes MCP requests to the server for the request's toolsets.
func (h *HTTPServer) Handler() http.Handler {
	// Stateless: stateful mode keeps sessions per-instance, so behind a load
	// balancer a follow-up request routed to another replica 404s. The
	// instrumented helper sets the transport, needed for client attribution.
	startup := h.server.NewStreamableHTTPHandler(streamableOptions())
	mcpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, narrowed, err := requestToolsets(r, h.config.AllowedTools)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !narrowed {
			startup.ServeHTTP(w, r)
			return
		}
		handler, release, err := h.servers.handler(allowed)
		if err != nil {
			log.Printf("❌ build server for toolsets: %v", err)
			http.Error(w, "failed to build server for the requested toolsets", http.StatusInternalServerError)
			return
		}
		defer release()
		handler.ServeHTTP(w, r)
	})

	// Register handlers on both root and /mcp paths for maximum client flexibility
	mux := http.NewServeMux()
	mux.Handle("/", mcpHandler)    // Root endpoint for standard MCP clients
	mux.Handle("/mcp", mcpHandler) // /mcp endpoint for explicit MCP usage
	mux.HandleFunc("/health", h.handleHealth)
	return mux
}

func streamableOptions() *mcp.StreamableHTTPOptions {
	return &mcp.StreamableHTTPOptions{Stateless: true}
}

// requestToolsets returns the tools a request may see: the startup set
// intersected with the toolsets it asks for, so a request can narrow but never
// widen. narrowed is false when the request asks for nothing or for all.
func requestToolsets(r *http.Request, startup toolsets.Set) (toolsets.Set, bool, error) {
	spec := r.URL.Query().Get(toolsetsQueryParam)
	if values := r.Header.Values(toolsetsHeader); len(values) > 0 {
		spec = strings.Join(values, ",")
	}
	requested, err := toolsets.Parse(spec)
	if err != nil {
		return nil, false, err
	}
	if requested == nil {
		return startup, false, nil
	}
	if startup == nil {
		return requested, true, nil
	}
	effective := toolsets.Set{}
	for tool := range requested {
		if startup.Allows(tool) {
			effective[tool] = struct{}{}
		}
	}
	return effective, true, nil
}

// toolsetServers caches one MCP server per distinct effective tool set, keyed
// by its sorted tool names; like the startup server, each is shared by every
// stateless request for that set.
type toolsetServers struct {
	build     func(toolsets.Set) (*last9mcp.Last9MCPServer, error)
	maxCached int

	mu      sync.Mutex
	cached  map[string]http.Handler
	servers []*last9mcp.Last9MCPServer
}

// handler returns the handler for allowed and a release func the caller must
// run after serving; release shuts down a server built past the cache bound.
func (c *toolsetServers) handler(allowed toolsets.Set) (http.Handler, func(), error) {
	key := toolSetKey(allowed)
	c.mu.Lock()
	defer c.mu.Unlock()
	if h, ok := c.cached[key]; ok {
		return h, func() {}, nil
	}
	server, err := c.build(allowed)
	if err != nil {
		return nil, nil, err
	}
	h := server.NewStreamableHTTPHandler(streamableOptions())
	if len(c.cached) >= c.maxCached {
		return h, func() { shutdownServer(server) }, nil
	}
	c.cached[key] = h
	c.servers = append(c.servers, server)
	return h, func() {}, nil
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

func shutdownServer(server *last9mcp.Last9MCPServer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("❌ per-request MCP server shutdown error: %v", err)
	}
}

func toolSetKey(allowed toolsets.Set) string {
	names := make([]string, 0, len(allowed))
	for name := range allowed {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

// Start starts the HTTP server with streamable HTTP support
func (h *HTTPServer) Start() error {
	// url is host:port
	url := h.config.Host + ":" + h.config.Port

	// Create HTTP server with timeouts
	httpServer := &http.Server{
		Addr:         url,
		Handler:      h.Handler(),
		ReadTimeout:  constants.DefaultHTTPTimeout,
		WriteTimeout: constants.DefaultHTTPTimeout,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("🚀 MCP server listening on %s", url)

	// add shutdown hook
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	// add signal chan
	case sig := <-signalChan:
		log.Printf("🛑 Received signal: %v, initiating graceful shutdown...", sig)

	case err := <-serverErr:
		log.Printf("❌ Server error: %v", err)
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("❌ Graceful shutdown failed: %v", err)
		return err
	}
	log.Printf("✅ HTTP server shutdown complete")

	if err := errors.Join(h.server.Shutdown(shutdownCtx), h.servers.shutdown(shutdownCtx)); err != nil {
		log.Printf("❌ MCP server shutdown error: %v", err)
		return err
	}

	log.Printf("✅ MCP server shutdown complete")
	return nil
}

func (h *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"server":  "last9-mcp",
		"version": Version,
	})
}
