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

const GetLogServicesDescription = `Discover which services are actively sending logs to Last9, along with their valid environments, physical index, and severity levels.

Use this tool before get_logs or get_service_logs to:
- Confirm a service is actually ingesting logs (avoids querying for data that doesn't exist)
- Get the exact service_name and env values to use in log queries (prevents typos and wrong filters)
- Identify the physical index a service writes to — pass this as the index parameter to get_logs / get_service_logs for faster, targeted queries
- Understand which severity levels are present for a service

Parameters:
- service: (Optional) Filter by a specific service name. Omit to list all services.
- env: (Optional) Filter by environment (e.g. production, staging).

Returns a list of entries with: service_name, env, physical_index, severity.
`

// GetLogServicesArgs represents the input arguments for the get_log_services tool
type GetLogServicesArgs struct {
	Service string `json:"service,omitempty" jsonschema:"Optional service name to filter results"`
	Env     string `json:"env,omitempty" jsonschema:"Optional environment to filter results (e.g. production)"`
}

// LogServiceEntry represents one distinct combination of service, env, index and severity
type LogServiceEntry struct {
	ServiceName   string `json:"service_name"`
	Env           string `json:"env"`
	PhysicalIndex string `json:"physical_index"`
	Severity      string `json:"severity,omitempty"`
}

// NewGetLogServicesHandler creates a handler for the get_log_services tool
func NewGetLogServicesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetLogServicesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetLogServicesArgs) (*mcp.CallToolResult, any, error) {
		query := buildLogServicesQuery(args.Service, args.Env)

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

		entries := make([]LogServiceEntry, 0, len(results))
		for _, r := range results {
			name := r.Metric["name"]
			if name == "" {
				name = "default"
			}
			entries = append(entries, LogServiceEntry{
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

func buildLogServicesQuery(service, env string) string {
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
