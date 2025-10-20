package models

import "sync"

// Config holds the server configuration parameters
type Config struct {
	// Last9 connection settings
	AuthToken    string // API token for authentication
	BaseURL      string // Last9 API URL
	RefreshToken string // Refresh token for authentication

	// Rate limiting configuration
	RequestRateLimit float64 // Maximum requests per second
	RequestRateBurst int     // Maximum burst capacity for requests

	// HTTP server configuration
	HTTPMode bool   // Enable HTTP server mode instead of STDIO
	Port     string // HTTP server port
	Host     string // HTTP server host

	// Access token for authenticated requests
	AccessToken string
	OrgSlug     string // Organization slug for multi-tenant support
	APIBaseURL  string // Base URL for API requests
	// Prometheus configuration
	PrometheusReadURL  string // URL for Prometheus read API
	PrometheusUsername string // Username for Prometheus authentication
	PrometheusPassword string // Password for Prometheus authentication

	// Token refresh synchronization
	TokenMutex sync.RWMutex // Protects access token during refresh
}
