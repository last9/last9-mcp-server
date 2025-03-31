package models

// Config holds the server configuration parameters
type Config struct {
	// Last9 connection settings
	AuthToken    string // API token for authentication
	BaseURL      string // Last9 API URL
	ActionURL    string // Action URL for sending notifications
	RefreshToken string // Refresh token for authentication

	// Rate limiting configuration
	RequestRateLimit float64 // Maximum requests per second
	RequestRateBurst int     // Maximum burst capacity for requests
}
