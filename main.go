// An MCP server implementation for Last9 that enables AI agents
// to query exception and service graph data
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
	// The release image is alpine without tzdata; embed the zone database so
	// get_current_time's LoadLocation works wherever the binary runs.
	_ "time/tzdata"

	"github.com/joho/godotenv"
	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/peterbourgon/ff/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	l9telemetry "last9-mcp/internal/telemetry"
	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/utils"
	"last9-mcp/internal/workflows"
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
	fs.BoolVar(&cfg.UseLogSearchAPI, "use_log_search_api", false, "Route get_logs through the server-side log search API instead of client-side chunking")
	fs.Float64Var(&cfg.RequestRateLimit, "rate", 1, "Requests per second limit")
	fs.IntVar(&cfg.RequestRateBurst, "burst", 1, "Request burst capacity")
	fs.IntVar(&cfg.MaxGetLogsEntries, "max_get_logs_entries", models.DefaultMaxGetLogsEntries, "Maximum number of entries returned by chunked raw get_logs requests")
	fs.BoolVar(&cfg.HTTPMode, "http", false, "Run as HTTP server instead of STDIO")
	fs.StringVar(&cfg.Port, "port", "8080", "HTTP server port")
	fs.StringVar(&cfg.Host, "host", "localhost", "HTTP server host")
	filterFlags, err := bindToolFilterFlags(fs)
	if err != nil {
		return cfg, err
	}
	versionFlag := fs.Bool("version", false, "Print version information")

	var configFile string
	fs.StringVar(&configFile, "config", "", "config file path")

	err = ff.Parse(fs, os.Args[1:],
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

	cfg.Toolsets = filterFlags.toolsets
	if cfg.ToolFilter, err = filterFlags.filter(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// toolFilterFlags holds the flags that decide the served tool surface. Serving
// and dump-tools both bind them, so a dump matches what a deployment serves.
type toolFilterFlags struct {
	toolsets   string
	readOnly   bool
	allowWrite string
}

func bindToolFilterFlags(fs *flag.FlagSet) (*toolFilterFlags, error) {
	readOnly, err := toolsets.ReadOnlyFromEnv()
	if err != nil {
		return nil, err
	}
	f := &toolFilterFlags{}
	fs.StringVar(&f.toolsets, "toolsets", toolsets.SpecFromEnv(), "Comma-separated MCP toolsets to expose (logs,traces,metrics,alerts,dashboards,profiles,grafana,investigate,all). Empty or all = full surface")
	fs.BoolVar(&f.readOnly, "read-only", readOnly, "Serve only read-only tools (env LAST9_MCP_READ_ONLY)")
	fs.StringVar(&f.allowWrite, "allow-write-tools", toolsets.AllowWriteSpecFromEnv(), "Comma-separated write tools still served with --read-only (env LAST9_MCP_ALLOW_WRITE_TOOLS)")
	return f, nil
}

func (f *toolFilterFlags) filter() (toolsets.Filter, error) {
	return toolsets.NewFilter(f.toolsets, f.readOnly, f.allowWrite)
}

// parseDumpToolFilter loads .env then parses the tool filter flags and env for
// dump-tools (which short-circuits before SetupConfig and needs no credentials).
func parseDumpToolFilter(args []string) (toolsets.Filter, error) {
	_ = godotenv.Load()
	fs := flag.NewFlagSet("dump-tools", flag.ContinueOnError)
	flags, err := bindToolFilterFlags(fs)
	if err != nil {
		return toolsets.Filter{}, err
	}
	if err := fs.Parse(args); err != nil {
		return toolsets.Filter{}, err
	}
	return flags.filter()
}

// toolsetServerBuilder returns what --http calls to build the server for a
// request's narrowed toolsets: cfg with only the toolsets replaced.
func toolsetServerBuilder(cfg models.Config) func(toolsets.Set) (*last9mcp.Last9MCPServer, error) {
	return func(allowed toolsets.Set) (*last9mcp.Last9MCPServer, error) {
		c := cfg
		c.ToolFilter.Tools = allowed
		return buildServer(c)
	}
}

// buildServer creates the served MCP server for cfg.ToolFilter: resources,
// prompts, tools and the instructions that name them.
func buildServer(cfg models.Config) (*last9mcp.Last9MCPServer, error) {
	info := &mcp.Implementation{Name: "last9-mcp", Version: Version}
	cfg.MCPServerInfo = info
	server, err := last9mcp.NewServerWithOptions(info.Name, info.Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP server: %w", err)
	}
	registerReferenceResources(server)
	workflows.Register(server)
	if err := registerToolsAndInstructions(server, cfg); err != nil {
		return nil, fmt.Errorf("failed to build the served surface: %w", err)
	}
	return server, nil
}

func main() {
	os.Exit(run())
}

// serveStdio serves one stdio session. A closed stdin or a cancelled ctx is a
// normal exit, not an error.
func serveStdio(ctx context.Context, server *last9mcp.Last9MCPServer, t mcp.Transport) error {
	server.Server.AddReceivingMiddleware(cancelRequestsOn(ctx))
	err := server.Serve(ctx, t)
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// cancelRequestsOn cancels in-flight requests when shutdown is cancelled. The
// SDK hides session cancellation from handlers and waits for them on close,
// while refusing to write their replies, so draining would only delay the exit.
func cancelRequestsOn(shutdown context.Context) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			defer context.AfterFunc(shutdown, cancel)()
			return next(ctx, method, req)
		}
	}
}

func run() int {
	// dump-tools runs before full config parsing: it needs no credentials
	// and must work in CI and eval harnesses without a refresh token.
	// It still honors --toolsets, --read-only and --allow-write-tools and
	// their env vars (and loads .env) so snapshots match the served surface.
	if len(os.Args) > 1 && os.Args[1] == "dump-tools" {
		filter, err := parseDumpToolFilter(os.Args[2:])
		if err != nil {
			log.Fatalf("dump-tools failed: %v", err)
		}
		if err := dumpTools(os.Stdout, filter); err != nil {
			log.Fatalf("dump-tools failed: %v", err)
		}
		return 0
	}

	if len(os.Args) > 1 && os.Args[1] == "dump-prompts" {
		if err := dumpPrompts(os.Stdout); err != nil {
			log.Fatalf("dump-prompts failed: %v", err)
		}
		return 0
	}

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

	// Auth and API config must come before OTel init so tenant/cluster IDs
	// are available as resource attributes on all spans and metrics.
	tokenManager, err := auth.NewTokenManager(cfg.RefreshToken)
	if err != nil {
		log.Fatalf("failed to create token manager: %v", err)
	}

	cfg.TokenManager = tokenManager
	if err := utils.PopulateAPICfg(&cfg); err != nil {
		log.Fatalf("failed to populate API config: %v", err)
	}

	if cfg.DisableTelemetry {
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
	} else {
		shutdown, err := l9telemetry.InitProviders(context.Background(), Version, cfg.OrgSlug, cfg.ClusterID)
		if err != nil {
			log.Fatalf("failed to init telemetry: %v", err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := shutdown(ctx); err != nil {
				slog.Error("telemetry shutdown error", "error", err)
			}
		}()
	}

	slog.Info("config loaded",
		"http_mode", cfg.HTTPMode,
		"max_get_logs_entries", cfg.MaxGetLogsEntries,
		"telemetry_disabled", cfg.DisableTelemetry,
		"version", Version,
	)

	// From here on, return instead of log.Fatal: os.Exit would skip the deferred telemetry flush.
	server, err := buildServer(cfg)
	if err != nil {
		slog.Error("failed to build server", "error", err)
		return 1
	}

	if !cfg.DisableTelemetry {
		meter := otel.GetMeterProvider().Meter("last9-mcp")
		serverInfo, err := meter.Int64ObservableGauge(
			"last9_mcp_server_info",
			metric.WithDescription("MCP server version info; value is always 1, use labels for version tracking"),
		)
		if err != nil {
			slog.Warn("failed to create server info gauge", "error", err)
		} else if reg, err := meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
			o.ObserveInt64(serverInfo, 1,
				metric.WithAttributes(
					attribute.String("version", Version),
					attribute.String("commit", CommitSHA),
				),
			)
			return nil
		}, serverInfo); err != nil {
			slog.Warn("failed to register server info callback", "error", err)
		} else {
			defer reg.Unregister()
		}
	}

	if cfg.HTTPMode {
		httpServer := NewHTTPServer(server, cfg, toolsetServerBuilder(cfg))
		if err := httpServer.Start(); err != nil {
			slog.Error("HTTP server error", "error", err)
			return 1
		}
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Restore default signal handling once shutdown starts, so a second signal
	// still kills a process stuck in shutdown.
	context.AfterFunc(ctx, stop)
	if err := serveStdio(ctx, server, &mcp.StdioTransport{}); err != nil {
		slog.Error("stdio server error", "error", err)
		return 1
	}
	return 0
}
