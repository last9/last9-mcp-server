// An MCP server implementation for Last9 that enables AI agents
// to query exception and service graph data
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"last9-mcp/internal/models"

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

// setupConfig initializes and parses the configuration
func setupConfig() (models.Config, error) {
	fs := flag.NewFlagSet("last9-mcp", flag.ExitOnError)

	var cfg models.Config
	fs.StringVar(&cfg.AuthToken, "auth", os.Getenv("LAST9_AUTH_TOKEN"), "Last9 API auth token")
	fs.StringVar(&cfg.BaseURL, "url", os.Getenv("LAST9_BASE_URL"), "Last9 API URL")
	fs.StringVar(&cfg.RefreshToken, "refresh_token", os.Getenv("LAST9_REFRESH_TOKEN"), "Last9 refresh token for authentication")
	fs.Float64Var(&cfg.RequestRateLimit, "rate", 1, "Requests per second limit")
	fs.IntVar(&cfg.RequestRateBurst, "burst", 1, "Request burst capacity")

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

	if cfg.AuthToken == "" {
		return cfg, errors.New("Last9 auth token must be provided via LAST9_AUTH_TOKEN env var")
	}

	// Set default base URL if not provided
	if cfg.BaseURL == "" {
		return cfg, errors.New("Last9 base URL must be provided via LAST9_BASE_URL env var")
	}

	return cfg, nil
}
