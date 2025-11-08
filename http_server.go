package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"last9-mcp/internal/models"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPServer wraps the MCP server for HTTP transport
type HTTPServer struct {
	server   *last9mcp.Last9MCPServer
	config   models.Config
	toolsMap map[string]interface{}
	sessions map[string]*MCPSession
	mu       sync.RWMutex
}

// MCPSession represents an MCP session state
type MCPSession struct {
	ID           string
	Initialized  bool
	Capabilities map[string]interface{}
	CreatedAt    time.Time
}

// NewHTTPServer creates a new HTTP-based MCP server
func NewHTTPServer(server *last9mcp.Last9MCPServer, config models.Config) *HTTPServer {
	return &HTTPServer{
		server:   server,
		config:   config,
		sessions: make(map[string]*MCPSession),
	}
}

// Start starts the HTTP server with streamable HTTP support
func (h *HTTPServer) Start() error {
	// url is host:port
	url := h.config.Host + ":" + h.config.Port

	// Create a mux to handle multiple endpoints
	mux := http.NewServeMux()

	// Create stateless MCP handler for maximum client compatibility
	// Enables direct tool calls without session management - perfect for curl testing,
	// serverless deployments, and horizontal scaling per MCP team recommendations
	httpHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return h.server.Server
	}, nil)

	// Register handlers on both root and /mcp paths for maximum client flexibility
	mux.Handle("/", httpHandler)    // Root endpoint for standard MCP clients
	mux.Handle("/mcp", httpHandler) // /mcp endpoint for explicit MCP usage
	mux.HandleFunc("/health", h.handleHealth)

	// Create HTTP server with timeouts
	httpServer := &http.Server{
		Addr:         url,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
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

	if err := h.server.Shutdown(shutdownCtx); err != nil {
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
		"version": "1.0.0",
	})
}
