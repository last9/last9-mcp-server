package apm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxServiceProfileBodyBytes caps the success body. A profile is small and
// bounded; anything larger is drift, not data worth buffering.
const maxServiceProfileBodyBytes = 5 * 1024 * 1024

// briefUnavailableMarker replaces the brief when the response no longer parses,
// so a dropped routing hint is visible rather than silent.
const briefUnavailableMarker = "Service profile: brief unavailable — the response did not match the expected schema. Raw profile follows; derive routing from it directly."

// GetServiceProfileArgs are the input parameters for the get_service_profile tool.
type GetServiceProfileArgs struct {
	ServiceName string `json:"service_name" jsonschema:"(Required) Service to derive a telemetry profile for"`
	Datasource  string `json:"datasource,omitempty" jsonschema:"Name of the datasource to query. If omitted, uses the default configured datasource."`
}

type serviceProfileRequest struct {
	Service  string `json:"service"`
	ReadURL  string `json:"read_url"`
	Username string `json:"username"`
	Password string `json:"password"`
	Region   string `json:"region"`
}

// NewGetServiceProfileHandler returns the handler for the get_service_profile MCP tool.
func NewGetServiceProfileHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetServiceProfileArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetServiceProfileArgs) (*mcp.CallToolResult, any, error) {
		service := strings.TrimSpace(args.ServiceName)
		if service == "" {
			return nil, nil, fmt.Errorf("service_name is required")
		}

		queryCfg, err := resolveDatasourceCfg(cfg, strings.TrimSpace(args.Datasource))
		if err != nil {
			return nil, nil, err
		}

		payload := serviceProfileRequest{
			Service:  service,
			ReadURL:  queryCfg.PrometheusReadURL,
			Username: queryCfg.PrometheusUsername,
			Password: queryCfg.PrometheusPassword,
			Region:   queryCfg.Region,
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
		}

		apiURL := fmt.Sprintf("%s%s", queryCfg.APIBaseURL, constants.EndpointServiceProfile)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		if queryCfg.TokenManager == nil {
			return nil, nil, fmt.Errorf("token manager is not configured")
		}

		httpReq.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
		httpReq.Header.Set(constants.HeaderContentType, constants.HeaderContentTypeJSON)
		httpReq.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+queryCfg.TokenManager.GetAccessToken(ctx))

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch service profile: %w", err)
		}
		defer resp.Body.Close()

		// Request body carries Prometheus credentials; a raw echo of the upstream
		// error would surface them to the model.
		if resp.StatusCode != http.StatusOK {
			return nil, nil, utils.NewUpstreamHTTPError(resp, "service profile")
		}

		rawJSON, err := io.ReadAll(io.LimitReader(resp.Body, maxServiceProfileBodyBytes))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
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

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: text,
				},
			},
		}, nil, nil
	}
}
