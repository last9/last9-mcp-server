// An MCP server implementation for Last9 that enables AI agents
// to query exception and service graph data
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"last9-mcp/internal/attributes"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/joho/godotenv"
	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/peterbourgon/ff/v3"
	"go.opentelemetry.io/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// Version information
var (
	Version   = "dev"     // Set by goreleaser
	CommitSHA = "unknown" // Set by goreleaser
	BuildTime = "unknown" // Set by goreleaser
)

// setupConfig initializes and parses the configuration
func SetupConfig(defaults models.Config) (models.Config, error) {
	fs := flag.NewFlagSet("last9-mcp", flag.ExitOnError)

	var cfg models.Config
	fs.StringVar(&cfg.RefreshToken, "refresh_token", os.Getenv("LAST9_REFRESH_TOKEN"), "Last9 refresh token for authentication")
	fs.StringVar(&cfg.DatasourceName, "datasource", os.Getenv("LAST9_DATASOURCE"), "Datasource name to use (overrides default datasource)")
	fs.StringVar(&cfg.APIHost, "api_host", os.Getenv("LAST9_API_HOST"), "API host (defaults to app.last9.io)")
	fs.BoolVar(&cfg.DisableTelemetry, "disable_telemetry", true, "Disable OpenTelemetry tracing/metrics")
	fs.Float64Var(&cfg.RequestRateLimit, "rate", 1, "Requests per second limit")
	fs.IntVar(&cfg.RequestRateBurst, "burst", 1, "Request burst capacity")
	fs.IntVar(&cfg.MaxGetLogsEntries, "max_get_logs_entries", models.DefaultMaxGetLogsEntries, "Maximum number of entries returned by chunked raw get_logs requests")
	fs.BoolVar(&cfg.HTTPMode, "http", false, "Run as HTTP server instead of STDIO")
	fs.StringVar(&cfg.Port, "port", "8080", "HTTP server port")
	fs.StringVar(&cfg.Host, "host", "localhost", "HTTP server host")
	versionFlag := fs.Bool("version", false, "Print version information")

	var configFile string
	fs.StringVar(&configFile, "config", "", "config file path")

	err := ff.Parse(fs, os.Args[1:],
		ff.WithEnvVarPrefix("LAST9"),
		ff.WithConfigFileFlag("config"),
		ff.WithConfigFileParser(ff.JSONParser),
	)
	if err != nil {
		return cfg, fmt.Errorf("failed to parse configuration: %w", err)
	}

	if *versionFlag {
		fmt.Printf("Version: %s\nCommit: %s\nBuild Time: %s\n", Version, CommitSHA, BuildTime)
		os.Exit(0)
	}

	if cfg.RefreshToken == "" {
		if defaults.RefreshToken != "" {
			cfg.RefreshToken = defaults.RefreshToken
		} else {
			return cfg, errors.New("Last9 refresh token must be provided via LAST9_REFRESH_TOKEN env var")
		}
	}
	if cfg.MaxGetLogsEntries <= 0 {
		cfg.MaxGetLogsEntries = models.DefaultMaxGetLogsEntries
	}

	return cfg, nil
}

func main() {
	log.Printf("Starting Last9 MCP Server v%s", Version)

	// Load .env file if it exists (ignore errors if file doesn't exist)
	if err := godotenv.Load(); err != nil {
		log.Printf("No .env file found or error loading it (this is ok): %v", err)
	}

	cfg, err := SetupConfig(models.Config{})
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	// OTEL_SDK_DISABLED is the standard OTel env var. Honour it explicitly so
	// that users can override the default (disable_telemetry=true) without
	// needing the LAST9_DISABLE_TELEMETRY env var.
	if v := os.Getenv("OTEL_SDK_DISABLED"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.DisableTelemetry = parsed
		}
	}
	log.Printf(
		"Config loaded - HTTPMode: %t, Authentication: enabled, MaxGetLogsEntries: %d",
		cfg.HTTPMode,
		cfg.MaxGetLogsEntries,
	)

	tokenManager, err := auth.NewTokenManager(cfg.RefreshToken)
	if err != nil {
		log.Fatalf("failed to create token manager: %v", err)
	}

	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		log.Fatalf("failed to refresh access token: %v", err)
	}

	// Create attribute cache and perform best-effort initial fetch
	attrCache := attributes.NewAttributeCache(auth.GetHTTPClient(), cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	attrCache.Warm(ctx)
	cancel()

	if cfg.DisableTelemetry {
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
	}

	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version,
		last9mcp.WithSkipProviderInit(),
	)
	if err != nil {
		log.Fatalf("failed to create MCP server: %v", err)
	}

	// Register all tools
	if err := registerAllTools(server, cfg, attrCache); err != nil {
		log.Fatalf("failed to register tools: %v", err)
	}

	// Background goroutine to refresh attributes and re-register tools periodically
	go func() {
		ticker := time.NewTicker(2 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := attrCache.RefreshIfStale(refreshCtx); err != nil {
				log.Printf("Warning: failed to refresh attribute cache: %v", err)
			} else {
				// Re-register tools with updated descriptions (AddTool is an upsert)
				if err := registerAllTools(server, cfg, attrCache); err != nil {
					log.Printf("Warning: failed to re-register tools after cache refresh: %v", err)
				} else {
					log.Println("Attribute cache refreshed and tools re-registered")
				}
			}
			refreshCancel()
		}
	}()

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
