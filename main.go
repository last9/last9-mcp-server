// An MCP server implementation for Last9 that enables AI agents
// to query exception and service graph data
package main

import (
	"log"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/acrmp/mcp"
)

func main() {
	cfg, err := utils.SetupConfig(models.Config{})
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		log.Fatalf("failed to refresh access token: %w", err)
	}
	tools, err := createTools(cfg)
	if err != nil {
		log.Fatalf("create tools: %v", err)
	}

	info := mcp.Implementation{
		Name:    "last9-mcp",
		Version: utils.Version,
	}

	if cfg.HTTPMode {
		// Start HTTP server
		httpServer := NewHTTPServer(info, tools, cfg)
		log.Fatal(httpServer.Start())
	} else {
		// Start STDIO server (default)
		s := mcp.NewServer(info, tools)
		s.Serve()
	}
}
