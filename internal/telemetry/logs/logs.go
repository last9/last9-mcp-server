package logs

import (
	"fmt"
	"last9-mcp/internal/models"
	"net/http"

	"github.com/acrmp/mcp"
)

// NewGetLogsHandler creates a handler for getting logs that internally uses the get_service_logs handler
// This provides backward compatibility while leveraging the more advanced v2 API with physical index optimization
func NewGetLogsHandler(client *http.Client, cfg models.Config) func(mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
	// Get the service logs handler
	serviceLogsHandler := NewGetServiceLogsHandler(client, cfg)

	return func(params mcp.CallToolRequestParams) (mcp.CallToolResult, error) {
		// Transform get_logs parameters to get_service_logs format for internal processing
		transformedParams := mcp.CallToolRequestParams{
			Arguments: make(map[string]interface{}),
		}

		// Copy all parameters from original request
		for key, value := range params.Arguments {
			transformedParams.Arguments[key] = value
		}

		// Handle backward compatibility for severity parameter
		// get_logs uses single "severity" string, get_service_logs uses "severity_filters" array
		if severity, ok := params.Arguments["severity"].(string); ok && severity != "" {
			// Convert single severity to array format for service_logs handler
			transformedParams.Arguments["severity_filters"] = []interface{}{severity}
			// Remove the old parameter to avoid conflicts
			delete(transformedParams.Arguments, "severity")
		}

		// If service_name is not provided (get_logs allows optional service),
		// we need to handle this case since get_service_logs requires it
		if serviceName, ok := params.Arguments["service_name"].(string); !ok || serviceName == "" {
			// For get_logs without service_name, we cannot use get_service_logs
			// Return an error asking for service_name to be specified
			return mcp.CallToolResult{}, fmt.Errorf("service_name parameter is required. For cross-service log queries, please specify a service name or use the Last9 web interface")
		}

		// Call the service logs handler with transformed parameters
		return serviceLogsHandler(transformedParams)
	}
}
