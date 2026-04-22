package models

import "last9-mcp/internal/auth"

const DefaultMaxGetLogsEntries = 5000
const DefaultMaxGetTracesEntries = 5000

// DatasourceInfo holds resolved credentials for a named datasource.
// Populated at startup from the /datasources API response and cached in Config.Datasources.
type DatasourceInfo struct {
	Name      string
	ReadURL   string
	Username  string
	Password  string
	Region    string
	ClusterID string
	IsDefault bool
}

// Config holds the server configuration parameters
type Config struct {
	// Last9 connection settings
	RefreshToken string // Refresh token for authentication
	Region       string // AWS region (e.g., us-east-1, ap-south-1)

	// Rate limiting configuration
	RequestRateLimit    float64 // Maximum requests per second
	RequestRateBurst    int     // Maximum burst capacity for requests
	MaxGetLogsEntries   int     // Maximum number of entries returned by chunked raw get_logs requests
	MaxGetTracesEntries int     // Maximum number of traces returned by chunked get_traces requests

	// HTTP server configuration
	HTTPMode bool   // Enable HTTP server mode instead of STDIO
	Port     string // HTTP server port
	Host     string // HTTP server host

	OrgSlug    string // Organization slug for multi-tenant support
	ActionURL  string
	APIBaseURL string // Base URL for API requests
	// Datasource configuration
	DatasourceName   string // Datasource name to use (overrides default datasource)
	APIHost          string // API host (defaults to app.last9.io)
	DisableTelemetry bool   // Disable OpenTelemetry tracing/metrics
	// Prometheus configuration
	PrometheusReadURL  string // URL for Prometheus read API
	PrometheusUsername string // Username for Prometheus authentication
	PrometheusPassword string // Password for Prometheus authentication

	ClusterID string // Cluster ID from datasource (for dashboard deep links)

	// Datasources holds all available datasources fetched at startup.
	// Used to resolve per-query datasource credentials without an extra API call.
	Datasources []DatasourceInfo

	TokenManager *auth.TokenManager // Manages authentication tokens
}

// ResolveDatasource looks up a datasource by name from the cached list.
// Returns the zero value and false when name is empty or not found.
func (cfg Config) ResolveDatasource(name string) (DatasourceInfo, bool) {
	if name == "" {
		return DatasourceInfo{}, false
	}
	for _, ds := range cfg.Datasources {
		if ds.Name == name {
			return ds, true
		}
	}
	return DatasourceInfo{}, false
}
