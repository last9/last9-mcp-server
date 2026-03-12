package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"last9-mcp/internal/models"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const httpLogPreviewLimit = 160

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

	httpHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return h.server.Server
	}, nil)

	// Register handlers on both root and /mcp paths for maximum client flexibility
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.serveMCP(httpHandler, w, r)
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		h.serveMCP(httpHandler, w, r)
	})
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

func (h *HTTPServer) serveMCP(handler http.Handler, w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	log.Printf("📥 HTTP request started: %s %s (accept: %q, content_type: %q, session_id: %q, protocol_version: %q, remote_addr: %q)",
		r.Method,
		r.URL.Path,
		r.Header.Get("Accept"),
		r.Header.Get("Content-Type"),
		r.Header.Get("Mcp-Session-Id"),
		r.Header.Get("MCP-Protocol-Version"),
		r.RemoteAddr,
	)

	rw := &loggingResponseWriter{ResponseWriter: w}
	handler.ServeHTTP(rw, r)

	log.Printf("🏁 HTTP request completed: %s %s (status: %d, content_type: %q, bytes: %d, writes: %d, flushes: %d, duration: %v)",
		r.Method,
		r.URL.Path,
		rw.statusCode(),
		rw.Header().Get("Content-Type"),
		rw.bytesWritten,
		rw.writeCount,
		rw.flushCount,
		time.Since(start),
	)
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int
	writeCount   int
	flushCount   int
}

func (w *loggingResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *loggingResponseWriter) WriteHeader(statusCode int) {
	if w.status == 0 {
		w.status = statusCode
		log.Printf("📤 HTTP response headers: %d (content_type: %q, session_id: %q)",
			statusCode,
			w.Header().Get("Content-Type"),
			w.Header().Get("Mcp-Session-Id"),
		)
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *loggingResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytesWritten += n
	w.writeCount++
	log.Printf("📦 HTTP response write: %d bytes (total: %d, preview: %q)",
		n,
		w.bytesWritten,
		httpLogPreview(p[:n]),
	)
	return n, err
}

func (w *loggingResponseWriter) Flush() {
	w.flushCount++
	log.Printf("💧 HTTP response flushed (count: %d, total_bytes: %d)", w.flushCount, w.bytesWritten)
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hj.Hijack()
}

func (w *loggingResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *loggingResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	readerFrom, ok := w.ResponseWriter.(io.ReaderFrom)
	if !ok {
		return io.Copy(w.ResponseWriter, r)
	}
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := readerFrom.ReadFrom(r)
	w.bytesWritten += int(n)
	w.writeCount++
	log.Printf("📦 HTTP response streamed: %d bytes (total: %d)", n, w.bytesWritten)
	return n, err
}

func (w *loggingResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func httpLogPreview(p []byte) string {
	preview := bytes.TrimSpace(p)
	if len(preview) > httpLogPreviewLimit {
		preview = preview[:httpLogPreviewLimit]
	}
	return strings.ReplaceAll(string(preview), "\n", "\\n")
}
