package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/gorilla/websocket"
	"github.com/sourcegraph/jsonrpc2"
)

// HTTPServer wraps the MCP server for HTTP transport
// TODO: Update for new MCP SDK - currently disabled
type HTTPServer struct {
	info     mcp.Implementation
	tools    []interface{} // TODO: Update to new SDK types
	toolsMap map[string]interface{} // TODO: Update to new SDK types
	config   models.Config
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
// TODO: Update for new MCP SDK - currently disabled
func NewHTTPServer(info mcp.Implementation, tools []interface{}, config models.Config) *HTTPServer {
	toolsMap := make(map[string]interface{})
	// TODO: Update for new SDK - tool structure has changed
	/*
	for _, tool := range tools {
		toolsMap[tool.Metadata.Name] = tool
	}
	*/

	return &HTTPServer{
		info:     info,
		tools:    tools,
		toolsMap: toolsMap,
		config:   config,
		sessions: make(map[string]*MCPSession),
	}
}

// Start starts the HTTP server
func (h *HTTPServer) Start() error {
	addr := fmt.Sprintf("%s:%s", h.config.Host, h.config.Port)

	http.HandleFunc("/mcp", h.handleMCP)
	http.HandleFunc("/ws", h.handleWebSocket)
	http.HandleFunc("/health", h.handleHealth)

	log.Printf("Starting HTTP MCP server on %s", addr)
	return http.ListenAndServe(addr, nil)
}

// handleHealth provides a simple health check endpoint
func (h *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"server":  h.info.Name,
		"version": h.info.Version,
	})
}

// handleMCP handles the main MCP endpoint
func (h *HTTPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Mcp-Session-Id")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" && r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get session ID from header
	sessionID := r.Header.Get("Mcp-Session-Id")

	if r.Method == "POST" {
		h.handlePOST(w, r, sessionID)
	} else {
		h.handleGET(w, r, sessionID)
	}
}

// handlePOST processes JSON-RPC requests
func (h *HTTPServer) handlePOST(w http.ResponseWriter, r *http.Request, sessionID string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse JSON-RPC request
	var req jsonrpc2.Request
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON-RPC request", http.StatusBadRequest)
		return
	}

	// Process the MCP request
	response, shouldRespond := h.handleMCPRequest(&req, sessionID)

	// Send response only if needed (not for notifications)
	if shouldRespond {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("Error encoding response: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	} else {
		// For notifications, just return 200 OK with no body
		w.WriteHeader(http.StatusOK)
	}
}

// handleMCPRequest processes MCP protocol requests
func (h *HTTPServer) handleMCPRequest(req *jsonrpc2.Request, sessionID string) (jsonrpc2.Response, bool) {
	var resp jsonrpc2.Response
	resp.ID = req.ID

	switch req.Method {
	case "initialize":
		result := map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    h.info.Name,
				"version": h.info.Version,
			},
		}
		resultBytes, _ := json.Marshal(result)
		resp.Result = (*json.RawMessage)(&resultBytes)
		return resp, true

	case "notifications/initialized":
		// No response needed for notifications
		return jsonrpc2.Response{}, false

	case "ping":
		result := map[string]interface{}{}
		resultBytes, _ := json.Marshal(result)
		resp.Result = (*json.RawMessage)(&resultBytes)
		return resp, true

	case "tools/list":
		// TODO: Update for new MCP SDK - currently disabled
		result := map[string]interface{}{
			"tools": []interface{}{}, // Empty list since HTTP mode is disabled
		}
		resultBytes, _ := json.Marshal(result)
		resp.Result = (*json.RawMessage)(&resultBytes)
		return resp, true

	case "tools/call":
		h.handleToolCall(req, &resp)
		return resp, true


	default:
		resp.Error = &jsonrpc2.Error{
			Code:    jsonrpc2.CodeMethodNotFound,
			Message: fmt.Sprintf("Method not found: %s", req.Method),
		}
		return resp, true
	}
}

// handleToolCall executes a tool and returns the result
// TODO: Update for new MCP SDK - currently disabled
func (h *HTTPServer) handleToolCall(req *jsonrpc2.Request, resp *jsonrpc2.Response) {
	var params map[string]interface{} // TODO: Update to new SDK types

	if req.Params != nil {
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			resp.Error = &jsonrpc2.Error{
				Code:    jsonrpc2.CodeInvalidParams,
				Message: "Invalid tool call parameters",
			}
			return
		}
	}

	// TODO: Update for new MCP SDK - currently disabled
	toolName, ok := params["name"].(string)
	if !ok {
		resp.Error = &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: "Missing tool name",
		}
		return
	}

	_, exists := h.toolsMap[toolName]
	if !exists {
		resp.Error = &jsonrpc2.Error{
			Code:    jsonrpc2.CodeMethodNotFound,
			Message: fmt.Sprintf("Tool not found: %s", toolName),
		}
		return
	}

	// TODO: Execute the tool with new SDK patterns
	resp.Error = &jsonrpc2.Error{
		Code:    jsonrpc2.CodeMethodNotFound,
		Message: "HTTP mode temporarily disabled - use STDIO mode",
	}
}

// handleGET handles GET requests (for session management)
func (h *HTTPServer) handleGET(w http.ResponseWriter, r *http.Request, sessionID string) {
	// For now, just return session info or create new session
	if sessionID == "" {
		// Create new session
		sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
		h.mu.Lock()
		h.sessions[sessionID] = &MCPSession{
			ID:        sessionID,
			CreatedAt: time.Now(),
		}
		h.mu.Unlock()
	}

	h.mu.RLock()
	session, exists := h.sessions[sessionID]
	h.mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessionId":   session.ID,
		"initialized": session.Initialized,
		"createdAt":   session.CreatedAt,
	})
}

// handleWebSocket handles WebSocket connections
func (h *HTTPServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// log the WebSocket upgrade request
	log.Printf("WebSocket upgrade requested from %s\n", r.RemoteAddr)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for now
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	sessionID := fmt.Sprintf("ws_%d", time.Now().UnixNano())
	h.mu.Lock()
	h.sessions[sessionID] = &MCPSession{
		ID:        sessionID,
		CreatedAt: time.Now(),
	}
	h.mu.Unlock()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var req jsonrpc2.Request
		if err := json.Unmarshal(message, &req); err != nil {
			log.Printf("Invalid JSON-RPC message: %v", err)
			continue
		}

		response, shouldRespond := h.handleMCPRequest(&req, sessionID)
		if shouldRespond {
			if err := conn.WriteJSON(response); err != nil {
				log.Printf("Failed to write response: %v", err)
				break
			}
		}
	}
}

