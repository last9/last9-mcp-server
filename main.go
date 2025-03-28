// An MCP server implementation for Last9 that enables AI agents
// to query exception and service graph data
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/acrmp/mcp"
	"github.com/peterbourgon/ff/v3"
)

func main() {
	cfg, err := setupConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	tools, err := createTools(cfg)
	if err != nil {
		log.Fatalf("create tools: %v", err)
	}

	info := mcp.Implementation{
		Name:    "last9-mcp",
		Version: Version,
	}

	s := mcp.NewServer(info, tools)
	s.Serve()
}

// Version information
var (
	Version   = "dev"     // Set by goreleaser
	CommitSHA = "unknown" // Set by goreleaser
	BuildTime = "unknown" // Set by goreleaser
)

// config holds the server configuration parameters
type config struct {
	// Last9 connection settings
	authToken string // API token for authentication
	baseURL   string // Last9 API URL

	// Rate limiting configuration
	requestRateLimit float64 // Maximum requests per second
	requestRateBurst int     // Maximum burst capacity for requests
}

// setupConfig initializes and parses the configuration
func setupConfig() (config, error) {
	fs := flag.NewFlagSet("last9-mcp", flag.ExitOnError)

	var cfg config
	fs.StringVar(&cfg.authToken, "auth", os.Getenv("LAST9_AUTH_TOKEN"), "Last9 API auth token")
	fs.StringVar(&cfg.baseURL, "url", os.Getenv("LAST9_BASE_URL"), "Last9 API URL")
	fs.Float64Var(&cfg.requestRateLimit, "rate", 1, "Requests per second limit")
	fs.IntVar(&cfg.requestRateBurst, "burst", 1, "Request burst capacity")

	var configFile string
	fs.StringVar(&configFile, "config", "", "config file path")

	err := ff.Parse(fs, os.Args[1:],
		ff.WithEnvVarPrefix("LAST9"),
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ff.PlainParser),
	)
	if err != nil {
		return cfg, fmt.Errorf("failed to parse configuration: %w", err)
	}

	if cfg.authToken == "" {
		return cfg, errors.New("Last9 auth token must be provided via LAST9_AUTH_TOKEN env var")
	}

	// Set default base URL if not provided
	if cfg.baseURL == "" {
		return cfg, errors.New("Last9 base URL must be provided via LAST9_BASE_URL env var")
	}

	return cfg, nil
}
