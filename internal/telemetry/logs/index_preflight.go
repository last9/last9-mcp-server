package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const GetLoggingServicesDescription = `Discover which services are actively sending logs to Last9.

Returns the exact service_name, env, physical_index, and severity values present in the log
ingestion pipeline. Use this BEFORE calling get_logs or get_service_logs to:

1. Confirm a service is actually ingesting logs — if it doesn't appear here, no log query will
   return results (check drop rules next with get_drop_rules).
2. Get the exact spelling of service_name and env — prevents silent empty results from typos.
3. Obtain the physical_index for the service — pass it as the index parameter to get_logs /
   get_service_logs for faster queries that skip unrelated indexes.
4. Know which severity levels exist — avoids filtering for severities with no data.

Parameters:
- service: (Optional) Filter by service name. Omit to list all services sending logs.
- env: (Optional) Filter by environment (e.g. production, staging). Omit for all environments.

Call with no parameters to get a full map of what is ingesting logs and where.

Examples:
- Before "show errors for checkout service" → call with service="checkout" to confirm it exists
  and find its physical_index and valid env values.
- "Which services are sending logs?" → call with no parameters.
- "Are there logs for api in production?" → call with service="api", env="production".
`

// GetLoggingServicesArgs represents the input arguments for the get_logging_services tool
type GetLoggingServicesArgs struct {
	Service string `json:"service,omitempty" jsonschema:"Optional service name to filter results"`
	Env     string `json:"env,omitempty" jsonschema:"Optional environment to filter results (e.g. production)"`
}

// LoggingServiceEntry represents one distinct combination of service, env, index and severity
type LoggingServiceEntry struct {
	ServiceName   string `json:"service_name"`
	Env           string `json:"env"`
	PhysicalIndex string `json:"physical_index"`
	Severity      string `json:"severity,omitempty"`
}

// NewGetLoggingServicesHandler creates a handler for the get_logging_services tool
func NewGetLoggingServicesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLoggingServicesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLoggingServicesArgs) (*mcp.CallToolResult, any, error) {
		query := buildLoggingServicesQuery(args.Service, args.Env)

		resp, err := utils.MakePromInstantAPIQuery(ctx, client, query, time.Now().UTC().Unix(), cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to query log services: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, nil, fmt.Errorf("log services query failed with status %d: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
		}

		var results []struct {
			Metric map[string]string `json:"metric"`
		}
		if err := json.Unmarshal(body, &results); err != nil {
			return nil, nil, fmt.Errorf("failed to parse response: %w", err)
		}

		entries := make([]LoggingServiceEntry, 0, len(results))
		for _, r := range results {
			name := r.Metric["name"]
			if name == "" {
				name = "default"
			}
			entries = append(entries, LoggingServiceEntry{
				ServiceName:   r.Metric["service_name"],
				Env:           r.Metric["env"],
				PhysicalIndex: "physical_index:" + name,
				Severity:      r.Metric["severity"],
			})
		}

		out, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(out)},
			},
		}, nil, nil
	}
}

func buildLoggingServicesQuery(service, env string) string {
	filter := ""
	if service != "" && env != "" {
		filter = fmt.Sprintf(`{service_name=%q,env=%q}`, service, env)
	} else if service != "" {
		filter = fmt.Sprintf(`{service_name=%q}`, service)
	} else if env != "" {
		filter = fmt.Sprintf(`{env=%q}`, env)
	}
	return "physical_index_service_count" + filter
}
