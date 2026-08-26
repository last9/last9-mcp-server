package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultSearchLimit = 25
	maxSearchLimit     = 100
	maxSearchFetch     = 500
)

// SearchInfrastructureEntitiesArgs are the input parameters for search_infrastructure_entities.
type SearchInfrastructureEntitiesArgs struct {
	EntityType string `json:"entity_type" jsonschema:"(Required) Entity type: host, k8s_cluster, k8s_node, or k8s_pod."`
	Query      string `json:"query,omitempty" jsonschema:"Optional substring filter on the entity name."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Page size (default 25, max 100)."`
	Cursor     string `json:"cursor,omitempty" jsonschema:"Opaque cursor from the previous page's next_cursor."`
	Timestamp  int64  `json:"timestamp,omitempty" jsonschema:"Unix seconds of the observation window. Defaults to now."`
	ClusterID  string `json:"cluster_id,omitempty" jsonschema:"Levitate datasource UUID used in entity ids. Defaults to the configured datasource."`
}

type searchPage struct {
	EntityType string         `json:"entity_type"`
	Entities   []searchEntity `json:"entities"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Truncated  bool           `json:"truncated,omitempty"`
}

type searchEntity struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Attributes map[string]string `json:"attributes"`
	UI         searchUI          `json:"ui"`
}

type searchUI struct {
	Href string `json:"href,omitempty"`
}

type searchQuery struct {
	client *http.Client
	cfg    models.Config
	args   SearchInfrastructureEntitiesArgs
}

// NewSearchInfrastructureEntitiesHandler returns the handler for search_infrastructure_entities.
func NewSearchInfrastructureEntitiesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, SearchInfrastructureEntitiesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args SearchInfrastructureEntitiesArgs) (*mcp.CallToolResult, any, error) {
		page, err := runSearch(ctx, searchQuery{client: client, cfg: cfg, args: args})
		if err != nil {
			return nil, nil, err
		}
		body, err := json.Marshal(page)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal search result: %w", err)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(body)}}}, nil, nil
	}
}

func runSearch(ctx context.Context, q searchQuery) (searchPage, error) {
	entityType := strings.TrimSpace(q.args.EntityType)
	if err := validateSearchType(entityType); err != nil {
		return searchPage{}, err
	}
	if err := validateSearchConfig(q.cfg); err != nil {
		return searchPage{}, err
	}
	ts := q.args.Timestamp
	if ts <= 0 {
		ts = time.Now().Unix()
	}
	metrics, truncated, err := fetchSearchMetrics(ctx, q, ts)
	if err != nil {
		return searchPage{}, err
	}
	clusterID := firstNonEmpty(q.args.ClusterID, q.cfg.ClusterID)
	entities := entitiesFromMetrics(entityType, clusterID, q.cfg.OrgSlug, metrics)
	entities = filterEntities(entities, q.args.Query)
	return paginateEntities(entityType, entities, clampSearchLimit(q.args.Limit), q.args.Cursor, truncated)
}

func validateSearchConfig(cfg models.Config) error {
	if cfg.TokenManager == nil {
		return errors.New("token manager is not configured")
	}
	if strings.TrimSpace(cfg.PrometheusReadURL) == "" {
		return errors.New("prometheus read URL is not configured")
	}
	return nil
}

func validateSearchType(entityType string) error {
	switch entityType {
	case "host", "k8s_cluster", "k8s_node", "k8s_pod":
		return nil
	case "":
		return errors.New("entity_type is required")
	default:
		return fmt.Errorf("unsupported entity_type %q; use host, k8s_cluster, k8s_node, or k8s_pod", entityType)
	}
}

func clampSearchLimit(n int) int {
	if n <= 0 {
		return defaultSearchLimit
	}
	if n > maxSearchLimit {
		return maxSearchLimit
	}
	return n
}
