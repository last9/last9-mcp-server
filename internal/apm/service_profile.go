package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/last9api"
	apiservices "last9-mcp/internal/last9api/services"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/singleflight"
)

// briefUnavailableMarker replaces the brief when the response no longer parses,
// so a dropped routing hint is visible rather than silent.
const briefUnavailableMarker = "Service profile: brief unavailable — the response did not match the expected schema. Raw profile follows; derive routing from it directly."

const (
	serviceProfileCacheTTL = 30 * time.Second
	serviceProfileCacheMax = 128
)

type serviceProfileCacheEntry struct {
	raw       []byte
	expiresAt time.Time
}

// GetServiceProfileArgs are the input parameters for the get_service_profile tool.
type GetServiceProfileArgs struct {
	ServiceName string `json:"service_name" jsonschema:"(Required) Service to derive a telemetry profile for"`
	Datasource  string `json:"datasource,omitempty" jsonschema:"Datasource name; omitted uses the default, a near miss its closest match (named in an additional content block)."`
}

// NewGetServiceProfileHandler returns the handler for the get_service_profile MCP tool.
func NewGetServiceProfileHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetServiceProfileArgs) (*mcp.CallToolResult, any, error) {
	var cacheMu sync.Mutex
	cache := make(map[string]serviceProfileCacheEntry)
	var fetchGroup singleflight.Group

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
		cacheKey := choice.name + "\x00" + service
		now := utils.Now()
		cacheMu.Lock()
		cached, cacheHit := cache[cacheKey]
		cacheMu.Unlock()
		var rawJSON []byte
		if cacheHit && now.Before(cached.expiresAt) {
			rawJSON = append([]byte(nil), cached.raw...)
		} else {
			result := fetchGroup.DoChan(cacheKey, func() (any, error) {
				cacheMu.Lock()
				cached, cacheHit := cache[cacheKey]
				cacheMu.Unlock()
				if cacheHit && utils.Now().Before(cached.expiresAt) {
					return append([]byte(nil), cached.raw...), nil
				}
				fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.DefaultHTTPTimeout)
				defer cancel()
				fetched, err := apiservices.GetServiceProfile(fetchCtx, last9api.NewClient(client, queryCfg), payload)
				if err != nil {
					return nil, err
				}
				cacheMu.Lock()
				if len(cache) >= serviceProfileCacheMax {
					now := utils.Now()
					for key, entry := range cache {
						if now.After(entry.expiresAt) {
							delete(cache, key)
						}
					}
					if len(cache) >= serviceProfileCacheMax {
						for key := range cache {
							delete(cache, key)
							break
						}
					}
				}
				cache[cacheKey] = serviceProfileCacheEntry{raw: append([]byte(nil), fetched...), expiresAt: utils.Now().Add(serviceProfileCacheTTL)}
				cacheMu.Unlock()
				return fetched, nil
			})
			select {
			case fetched := <-result:
				err = fetched.Err
				if err == nil {
					rawJSON = fetched.Val.([]byte)
				}
			case <-ctx.Done():
				err = ctx.Err()
			}
		}
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
