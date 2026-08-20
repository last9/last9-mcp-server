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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxServiceProfileErrorBodyBytes = 2048

// GetServiceProfileArgs are the input parameters for the get_service_profile tool.
type GetServiceProfileArgs struct {
	ServiceName string `json:"service_name" jsonschema:"(Required) Service to derive a telemetry profile for"`
	Datasource  string `json:"datasource,omitempty" jsonschema:"Datasource name. Omit for default."`
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

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxServiceProfileErrorBodyBytes))
			msg := strings.TrimSpace(string(body))
			if msg == "" {
				msg = http.StatusText(resp.StatusCode)
			}
			return nil, nil, fmt.Errorf("service profile API returned status %d: %s", resp.StatusCode, msg)
		}

		rawJSON, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read response: %w", err)
		}

		var profile serviceProfileResponse
		if err := json.Unmarshal(rawJSON, &profile); err != nil {
			return nil, nil, fmt.Errorf("failed to parse response: %w", err)
		}

		text := formatInvestigationBrief(profile) + "\n\n" + string(rawJSON)

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: text,
				},
			},
		}, nil, nil
	}
}
