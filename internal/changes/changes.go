package changes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxLimit           = 500
	maxChangesResponse = 8 << 20
)

type GetChangesArgs struct {
	StartTime    string   `json:"start_time" jsonschema:"(Required) Absolute RFC3339 range start, inclusive"`
	EndTime      string   `json:"end_time" jsonschema:"(Required) Absolute RFC3339 range end"`
	Service      string   `json:"service,omitempty" jsonschema:"Exact service scope"`
	Environment  string   `json:"environment,omitempty" jsonschema:"Exact environment scope"`
	Cluster      string   `json:"cluster,omitempty" jsonschema:"Exact Kubernetes cluster scope"`
	Namespace    string   `json:"namespace,omitempty" jsonschema:"Exact Kubernetes namespace scope"`
	ResourceKind string   `json:"resource_kind,omitempty" jsonschema:"Exact Kubernetes resource kind"`
	ResourceName string   `json:"resource_name,omitempty" jsonschema:"Exact Kubernetes resource name"`
	ResourceUID  string   `json:"resource_uid,omitempty" jsonschema:"Exact Kubernetes resource UID"`
	Sources      []string `json:"sources,omitempty" jsonschema:"Sources to query; defaults to change_events and kubernetes_events"`
	Categories   []string `json:"categories,omitempty" jsonschema:"Change categories to include after source normalization"`
	Order        string   `json:"order,omitempty" jsonschema:"Chronological order: desc (default) or asc"`
	Cursor       string   `json:"cursor,omitempty" jsonschema:"Opaque cursor returned by a previous get_changes call"`
	Limit        int      `json:"limit,omitempty" jsonschema:"Maximum changes to return; API default when omitted, range 1-500"`
}

type dependencies struct {
	client *http.Client
	cfg    models.Config
}

func NewGetChangesHandler(
	client *http.Client,
	cfg models.Config,
) func(context.Context, *mcp.CallToolRequest, GetChangesArgs) (*mcp.CallToolResult, any, error) {
	deps := dependencies{client: client, cfg: cfg}
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		args GetChangesArgs,
	) (*mcp.CallToolResult, any, error) {
		normalized, err := validateArgs(args)
		if err != nil {
			return nil, nil, err
		}
		body, err := fetchChanges(ctx, deps, normalized)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
}

func validateArgs(args GetChangesArgs) (GetChangesArgs, error) {
	args = normalizeArgs(args)
	if args.StartTime == "" {
		return args, fmt.Errorf("start_time is required")
	}
	if args.EndTime == "" {
		return args, fmt.Errorf("end_time is required")
	}
	start, err := time.Parse(time.RFC3339, args.StartTime)
	if err != nil {
		return args, fmt.Errorf("start_time must be RFC3339")
	}
	end, err := time.Parse(time.RFC3339, args.EndTime)
	if err != nil {
		return args, fmt.Errorf("end_time must be RFC3339")
	}
	if !end.After(start) {
		return args, fmt.Errorf("end_time must be after start_time")
	}
	return validateControls(args)
}

func validateControls(args GetChangesArgs) (GetChangesArgs, error) {
	if !hasScope(args) {
		return args, fmt.Errorf("at least one scope field is required")
	}
	if args.Order != "" && args.Order != "asc" && args.Order != "desc" {
		return args, fmt.Errorf("order must be asc or desc")
	}
	if args.Limit < 0 {
		return args, fmt.Errorf("limit must be positive")
	}
	if args.Limit > maxLimit {
		return args, fmt.Errorf("limit must be at most %d", maxLimit)
	}
	return args, nil
}

func normalizeArgs(args GetChangesArgs) GetChangesArgs {
	args.StartTime = strings.TrimSpace(args.StartTime)
	args.EndTime = strings.TrimSpace(args.EndTime)
	args.Service = strings.TrimSpace(args.Service)
	args.Environment = strings.TrimSpace(args.Environment)
	args.Cluster = strings.TrimSpace(args.Cluster)
	args.Namespace = strings.TrimSpace(args.Namespace)
	args.ResourceKind = strings.TrimSpace(args.ResourceKind)
	args.ResourceName = strings.TrimSpace(args.ResourceName)
	args.ResourceUID = strings.TrimSpace(args.ResourceUID)
	args.Order = strings.TrimSpace(args.Order)
	args.Cursor = strings.TrimSpace(args.Cursor)
	args.Sources = normalizedList(args.Sources)
	args.Categories = normalizedList(args.Categories)
	return args
}

func normalizedList(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func hasScope(args GetChangesArgs) bool {
	return args.Service != "" ||
		args.Environment != "" ||
		args.Cluster != "" ||
		args.Namespace != "" ||
		args.ResourceKind != "" ||
		args.ResourceName != "" ||
		args.ResourceUID != ""
}

func fetchChanges(ctx context.Context, deps dependencies, args GetChangesArgs) ([]byte, error) {
	endpoint := deps.cfg.APIBaseURL + constants.EndpointChanges
	request, err := http.NewRequestWithContext(
		ctx, http.MethodGet, endpoint+"?"+changesQuery(args, deps.cfg).Encode(), nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create changes request: %w", err)
	}
	setHeaders(ctx, request, deps.cfg)
	response, err := deps.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("changes request failed: %w", ctx.Err())
		}
		return nil, fmt.Errorf("changes request failed")
	}
	defer response.Body.Close()
	return readResponse(response)
}

func setHeaders(ctx context.Context, request *http.Request, cfg models.Config) {
	request.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	request.Header.Set(constants.HeaderUserAgent, constants.UserAgentLast9MCP)
	token := cfg.TokenManager.GetAccessToken(ctx)
	request.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+token)
}

func readResponse(response *http.Response) ([]byte, error) {
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("changes API returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxChangesResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read changes response: %w", err)
	}
	if len(body) > maxChangesResponse {
		return nil, fmt.Errorf("changes API response exceeded %d bytes", maxChangesResponse)
	}
	return body, nil
}

func changesQuery(args GetChangesArgs, cfg models.Config) url.Values {
	query := url.Values{}
	query.Set("start_time", args.StartTime)
	query.Set("end_time", args.EndTime)
	if cfg.Region != "" {
		query.Set("region", cfg.Region)
	}
	if cfg.DatasourceName != "" {
		query.Set("data_source_name", cfg.DatasourceName)
	}
	setScope(query, args)
	setOptionalControls(query, args)
	return query
}

func setScope(query url.Values, args GetChangesArgs) {
	values := map[string]string{
		"service": args.Service, "environment": args.Environment, "cluster": args.Cluster,
		"namespace": args.Namespace, "resource_kind": args.ResourceKind,
		"resource_name": args.ResourceName, "resource_uid": args.ResourceUID,
	}
	for name, value := range values {
		if value != "" {
			query.Set(name, value)
		}
	}
}

func setOptionalControls(query url.Values, args GetChangesArgs) {
	for _, source := range args.Sources {
		query.Add("sources", source)
	}
	for _, category := range args.Categories {
		query.Add("categories", category)
	}
	if args.Order != "" {
		query.Set("order", args.Order)
	}
	if args.Cursor != "" {
		query.Set("cursor", args.Cursor)
	}
	if args.Limit > 0 {
		query.Set("limit", strconv.Itoa(args.Limit))
	}
}
