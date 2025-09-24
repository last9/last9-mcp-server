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

	"github.com/acrmp/mcp"
	"github.com/gorilla/websocket"
	"github.com/sourcegraph/jsonrpc2"
)

// HTTPServer wraps the MCP server for HTTP transport
type HTTPServer struct {
	info     mcp.Implementation
	tools    []mcp.ToolDefinition
	toolsMap map[string]mcp.ToolDefinition
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
func NewHTTPServer(info mcp.Implementation, tools []mcp.ToolDefinition, config models.Config) *HTTPServer {
	toolsMap := make(map[string]mcp.ToolDefinition)
	for _, tool := range tools {
		toolsMap[tool.Metadata.Name] = tool
	}

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
				"tools":   map[string]interface{}{},
				"prompts": map[string]interface{}{},
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
		tools := make([]mcp.Tool, len(h.tools))
		for i, tool := range h.tools {
			tools[i] = tool.Metadata
		}
		result := map[string]interface{}{
			"tools": tools,
		}
		resultBytes, _ := json.Marshal(result)
		resp.Result = (*json.RawMessage)(&resultBytes)
		return resp, true

	case "tools/call":
		h.handleToolCall(req, &resp)
		return resp, true

	case "prompts/list":
		h.handlePromptsList(req, &resp)
		return resp, true

	case "prompts/get":
		h.handlePromptsGet(req, &resp)
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
func (h *HTTPServer) handleToolCall(req *jsonrpc2.Request, resp *jsonrpc2.Response) {
	var params mcp.CallToolRequestParams

	if req.Params != nil {
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			resp.Error = &jsonrpc2.Error{
				Code:    jsonrpc2.CodeInvalidParams,
				Message: "Invalid tool call parameters",
			}
			return
		}
	}

	tool, exists := h.toolsMap[params.Name]
	if !exists {
		resp.Error = &jsonrpc2.Error{
			Code:    jsonrpc2.CodeMethodNotFound,
			Message: fmt.Sprintf("Tool not found: %s", params.Name),
		}
		return
	}

	// Execute the tool with rate limiting
	if tool.RateLimit != nil {
		if !tool.RateLimit.Allow() {
			resp.Error = &jsonrpc2.Error{
				Code:    -32000, // Custom error code for rate limiting
				Message: "Rate limit exceeded",
			}
			return
		}
	}

	// Execute the tool
	result, err := tool.Execute(params)
	if err != nil {
		resp.Error = &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInternalError,
			Message: err.Error(),
		}
		return
	}
	// Return the MCP result directly
	resultBytes, _ := json.Marshal(result)
	resp.Result = (*json.RawMessage)(&resultBytes)
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

// handlePromptsList returns the list of available prompts
func (h *HTTPServer) handlePromptsList(req *jsonrpc2.Request, resp *jsonrpc2.Response) {
	prompts := []map[string]interface{}{
		{
			"name":        "logjson_query_builder",
			"description": "Convert natural language log queries into structured JSON pipeline queries for log analysis",
			"arguments": []map[string]interface{}{
				{
					"name":        "natural_language_query",
					"description": "Natural language description of the log query to construct",
					"required":    true,
				},
				{
					"name":        "query_context",
					"description": "Additional context about the query (service, time range, specific fields to focus on)",
					"required":    false,
				},
			},
		},
	}

	result := map[string]interface{}{
		"prompts": prompts,
	}
	resultBytes, _ := json.Marshal(result)
	resp.Result = (*json.RawMessage)(&resultBytes)
}

// handlePromptsGet returns a specific prompt with its messages
func (h *HTTPServer) handlePromptsGet(req *jsonrpc2.Request, resp *jsonrpc2.Response) {
	var params map[string]interface{}
	if req.Params != nil {
		json.Unmarshal(*req.Params, &params)
	}

	promptName, ok := params["name"].(string)
	if !ok {
		resp.Error = &jsonrpc2.Error{
			Code:    jsonrpc2.CodeInvalidParams,
			Message: "Missing or invalid prompt name",
		}
		return
	}

	var messages []map[string]interface{}

	switch promptName {
	case "logjson_query_builder":
		naturalQuery := ""
		queryContext := ""

		if args, ok := params["arguments"].(map[string]interface{}); ok {
			if nq, ok := args["natural_language_query"].(string); ok {
				naturalQuery = nq
			}
			if qc, ok := args["query_context"].(string); ok {
				queryContext = qc
			}
		}

		messages = []map[string]interface{}{
			{
				"role": "system",
				"content": map[string]interface{}{
					"type": "text",
					"text": fmt.Sprintf(`You are a specialized log query construction assistant for Last9 observability platform. Your role is to translate natural language queries into structured JSON pipeline queries that can be executed by log analysis tools.

## Your Task:
Convert the following natural language query into a valid JSON pipeline format:
**Query:** %s
**Context:** %s

## JSON Pipeline Format:
You must return a JSON array containing operation objects. Available operations:

### 1. Filter Operations:
- **filter**: Filter logs based on conditions
- **parse**: Parse log content (json, regexp, logfmt)  
- **aggregate**: Perform aggregations (sum, avg, count, etc.)
- **window_aggregate**: Time-windowed aggregations
- **transform**: Transform/extract fields
- **select**: Select specific fields and apply limits (default limit: 20)

### 2. Field References:
- **Body**: Log message content
- **service**: Service name  
- **severity**: Log level (DEBUG, INFO, WARN, ERROR, FATAL)
- **attributes['field_name']**: Log/span attributes
- **resource_attributes['field_name']**: Resource attributes (prefixed with resource_)

### 3. Common OpenTelemetry Fields:
- **HTTP**: attributes['http.method'], attributes['http.status_code'], attributes['http.route']
- **Database**: attributes['db.system'], attributes['db.statement'], attributes['db.operation']
- **Messaging**: attributes['messaging.system'], attributes['messaging.destination']
- **RPC**: attributes['rpc.system'], attributes['rpc.method'], attributes['rpc.grpc.status_code']
- **Kubernetes**: resource_attributes['k8s.pod.name'], resource_attributes['k8s.namespace.name']
- **Cloud**: resource_attributes['cloud.provider'], resource_attributes['cloud.region']

### 4. Filter Operators:
- **$eq**: Equals
- **$neq**: Not equals  
- **$gt**: Greater than
- **$lt**: Less than
- **$gte**: Greater than or equal
- **$lte**: Less than or equal
- **$contains**: Contains text
- **$notcontains**: Doesn't contain text
- **$regex**: Regex match
- **$notnull**: Field exists
- **$and**: Multiple conditions (AND)
- **$or**: Multiple conditions (OR)

### 5. Aggregation Functions:
- **$sum**: Sum values
- **$avg**: Average values
- **$count**: Count records
- **$min**: Minimum value
- **$max**: Maximum value
- **$quantile**: Percentile calculation
- **$rate**: Rate calculation

### 6. Common Patterns:
- **5xx errors**: {"$and": [{"$gte": ["attributes['http.status_code']", 500]}, {"$lt": ["attributes['http.status_code']", 600]}]}
- **4xx errors**: {"$and": [{"$gte": ["attributes['http.status_code']", 400]}, {"$lt": ["attributes['http.status_code']", 500]}]}
- **Slow requests**: {"$gt": ["attributes['duration']", threshold_ms]}
- **Database errors**: {"$and": [{"$notnull": ["attributes['db.statement']"]}, {"$contains": ["Body", "error"]}]}
- **Authentication failures**: {"$or": [{"$eq": ["attributes['http.status_code']", 401]}, {"$contains": ["Body", "authentication failed"]}]}

### 7. Time Windows:
- **5 minutes**: ["5", "minutes"]
- **1 hour**: ["1", "hours"] 
- **1 day**: ["24", "hours"]

### 8. Grouping:
- **By service**: {"resource_attributes['service.name']": "service"}
- **By endpoint**: {"attributes['http.route']": "endpoint"}
- **By host**: {"resource_attributes['host.name']": "host"}
- **By namespace**: {"resource_attributes['k8s.namespace.name']": "namespace"}

### 9. Select Operations:
- **Limit results**: {"type": "select", "limit": 20}
- **Custom limit**: {"type": "select", "limit": 50}
- **No limit**: Omit select operation (returns all results)

## Instructions:
1. Analyze the natural language query carefully
2. Identify the required operations (filter, parse, aggregate, etc.)
3. Use appropriate field references and operators
4. Return ONLY a valid JSON array - no explanations
5. Ensure proper JSON syntax and structure
6. Chain operations logically: filter → parse → aggregate → select
7. Add a select operation with limit: 20 for result limiting (unless a different limit is specified)

## Example Output Format:
[{
  "type": "filter",
  "query": {
    "$contains": ["Body", "error"]
  }
}, {
  "type": "aggregate", 
  "function": {"$count": []},
  "as": "error_count",
  "groupby": {"service": "service"}
}, {
  "type": "select",
  "limit": 20
}]

Return only the JSON array for the given query.`, naturalQuery, queryContext),
				},
			},
		}

	default:
		resp.Error = &jsonrpc2.Error{
			Code:    jsonrpc2.CodeMethodNotFound,
			Message: fmt.Sprintf("Prompt not found: %s", promptName),
		}
		return
	}

	result := map[string]interface{}{
		"messages": messages,
	}
	resultBytes, _ := json.Marshal(result)
	resp.Result = (*json.RawMessage)(&resultBytes)
}
