package models

import "last9-mcp/internal/auth"

// Config holds the server configuration parameters
type Config struct {
	// Last9 connection settings
	RefreshToken string // Refresh token for authentication
	Region       string // AWS region (e.g., us-east-1, ap-south-1)

	// Rate limiting configuration
	RequestRateLimit float64 // Maximum requests per second
	RequestRateBurst int     // Maximum burst capacity for requests

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

	// SECURITY: Disables mutating tools (write/destroy operations) - DEFAULT: true
	// IMPORTANT: This flag is required until proper RBAC and authorization
	// is implemented on the UI. Flagged by VAPT team for security review.
	// Mutating tools like 'add_drop_rule' create/modify resources and
	// should only be enabled after authorization controls are in place.
	// Set LAST9_DISABLE_MUTATING_TOOLS=false to enable (NOT RECOMMENDED).
	DisableMutatingTools bool
	// Prometheus configuration
	PrometheusReadURL  string // URL for Prometheus read API
	PrometheusUsername string // Username for Prometheus authentication
	PrometheusPassword string // Password for Prometheus authentication

	ClusterID string // Cluster ID from datasource (for dashboard deep links)

	TokenManager *auth.TokenManager // Manages authentication tokens
}
