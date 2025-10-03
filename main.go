// An MCP server implementation for Last9 that enables AI agents
// to query exception and service graph data
package main

import (
	"context"
	"log"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	log.Printf("Starting Last9 MCP Server v%s", utils.Version)

	cfg, err := utils.SetupConfig(models.Config{})
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	log.Printf("Config loaded - BaseURL: %s, HTTPMode: %t", cfg.BaseURL, cfg.HTTPMode)

	if err := utils.PopulateAPICfg(&cfg); err != nil {
		log.Fatalf("failed to refresh access token: %v", err)
	}

	// Create MCP server with official SDK
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "last9-mcp",
		Version: utils.Version,
	}, nil)

	// Register all tools
	if err := registerAllTools(server, cfg); err != nil {
		log.Fatalf("failed to register tools: %v", err)
	}

	if cfg.HTTPMode {
		// TODO: HTTP server mode needs to be updated for new SDK
		log.Fatal("HTTP mode is temporarily disabled during SDK migration")
	} else {
		// Start STDIO server (default)
		if err := server.Run(context.Background(), mcp.NewStdioTransport()); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}
