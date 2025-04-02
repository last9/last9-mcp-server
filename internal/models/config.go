package models

// Config holds the server configuration parameters
type Config struct {
	// Last9 connection settings
	AuthToken    string // API token for authentication
	BaseURL      string // Last9 API URL
	RefreshToken string // Refresh token for authentication

	// Rate limiting configuration
	RequestRateLimit float64 // Maximum requests per second
	RequestRateBurst int     // Maximum burst capacity for requests
}
