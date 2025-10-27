// An MCP server implementation for Last9 that enables AI agents
// to query exception and service graph data
package main

import (
	"context"
	"log"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/joho/godotenv"
	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	log.Printf("Starting Last9 MCP Server v%s", utils.Version)

	// Load .env file if it exists (ignore errors if file doesn't exist)
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file found or error loading it (this is ok): %v", err)
	}

	cfg, err := utils.SetupConfig(models.Config{})
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	log.Printf("Config loaded - BaseURL: %s, HTTPMode: %t", cfg.BaseURL, cfg.HTTPMode)

	if err := utils.PopulateAPICfg(&cfg); err != nil {
		log.Fatalf("failed to refresh access token: %v", err)
	}

	// Create MCP server with new SDK
	server, err := last9mcp.NewServer("last9-mcp", utils.Version)
	if err != nil {
		log.Fatalf("failed to create MCP server: %v", err)
	}

	// Register all tools
	if err := registerAllTools(server, cfg); err != nil {
		log.Fatalf("failed to register tools: %v", err)
	}

	if cfg.HTTPMode {
		// Create HTTP server using NewHTTPServer
		httpServer := NewHTTPServer(server, cfg)

		// Start the server
		if err := httpServer.Start(); err != nil {
			log.Fatalf("HTTP server error: %v", err)
		}
	} else {
		// Start STDIO server (default)
		log.Fatal(server.Serve(context.Background(), &mcp.StdioTransport{}))
	}
}
