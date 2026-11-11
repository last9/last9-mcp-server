package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	apiservices "last9-mcp/internal/last9api/services"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// briefUnavailableMarker replaces the brief when the response no longer parses,
// so a dropped routing hint is visible rather than silent.
const briefUnavailableMarker = "Service profile: brief unavailable — the response did not match the expected schema. Raw profile follows; derive routing from it directly."

// GetServiceProfileArgs are the input parameters for the get_service_profile tool.
type GetServiceProfileArgs struct {
	ServiceName string `json:"service_name" jsonschema:"(Required) Service to derive a telemetry profile for"`
	Datasource  string `json:"datasource,omitempty" jsonschema:"Datasource name; omitted uses the default, a near miss its closest match (named in an additional content block)."`
}

// NewGetServiceProfileHandler returns the handler for the get_service_profile MCP tool.
func NewGetServiceProfileHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetServiceProfileArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetServiceProfileArgs) (*mcp.CallToolResult, any, error) {
		service := strings.TrimSpace(args.ServiceName)
		if service == "" {
			return nil, nil, fmt.Errorf("service_name is required")
		}

		choice, err := chooseDatasource(cfg, args.Datasource)
		if err != nil {
			return nil, nil, err
		}
		if choice.candidates != nil {
			return ambiguousDatasourceResult(args.Datasource, choice), nil, nil
		}
		queryCfg := choice.cfg

		payload := apiservices.GetServiceProfileInput{
			Service:  service,
			ReadURL:  queryCfg.PrometheusReadURL,
			Username: queryCfg.PrometheusUsername,
			Password: queryCfg.PrometheusPassword,
		}

		endRequest := utils.StartToolPhase(ctx, "get_service_profile", "request")
		rawJSON, err := apiservices.GetServiceProfile(ctx, last9api.NewClient(client, queryCfg), payload)
		endRequest(err)
		if err != nil {
			return nil, nil, err
		}

		// The raw JSON is the payload of record; parsing only builds the brief.
		// Backend schema drift must not fail a call the agent could still use —
		// but dropping the brief silently would hide that the routing guidance,
		// the reason to call this tool, is missing.
		text := string(rawJSON)
		var profile serviceProfileResponse
		if err := json.Unmarshal(rawJSON, &profile); err != nil {
			text = briefUnavailableMarker + "\n\n" + text
		} else {
			if profile.Service == "" {
				profile.Service = service
			}
			text = formatInvestigationBrief(profile) + "\n\n" + text
		}

		return withDatasourceNote(&mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: text,
				},
			},
		}, choice.note), nil, nil
	}
}
