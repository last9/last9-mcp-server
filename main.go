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
	log.Printf("Starting Last9 MCP Server v%s", utils.Version)

	cfg, err := utils.SetupConfig(models.Config{})
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	log.Printf("Config loaded - BaseURL: %s, HTTPMode: %t", cfg.BaseURL, cfg.HTTPMode)

	if err := utils.PopulateAPICfg(&cfg); err != nil {
		log.Fatalf("failed to refresh access token: %v", err)
	}
	log.Printf("API config populated successfully")

	tools, err := createTools(cfg)
	if err != nil {
		log.Fatalf("create tools: %v", err)
	}
	log.Printf("Tools created: %d tools registered", len(tools))

	info := mcp.Implementation{
		Name:    "last9-mcp",
		Version: utils.Version,
	}

	if cfg.HTTPMode {
		// Start HTTP server
		log.Printf("Starting HTTP server mode")
		httpServer := NewHTTPServer(info, tools, cfg)
		log.Fatal(httpServer.Start())
	} else {
		// Start STDIO server (default)
		log.Printf("Starting STDIO server mode")
		s := mcp.NewServer(info, tools)
		s.Serve()
	}
}
