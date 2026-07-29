package timeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"last9-mcp/internal/constants"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultLookbackMinutes = 60
	maxLookbackMinutes     = 60
	defaultMaxEvents       = 200
	maxEvents              = 500
	maxTimelineResponse    = 8 << 20

	kindChangeEvent  = "change_event"
	kindAlertEpisode = "alert_episode"
)

var supportedKinds = map[string]struct{}{
	kindChangeEvent:  {},
	kindAlertEpisode: {},
}

type GetChangeTimelineArgs struct {
	StartTimeISO    string   `json:"start_time_iso,omitempty" jsonschema:"RFC3339 range start; must be supplied with end_time_iso and span at most one hour"`
	EndTimeISO      string   `json:"end_time_iso,omitempty" jsonschema:"RFC3339 range end; must be supplied with start_time_iso and span at most one hour"`
	LookbackMinutes int      `json:"lookback_minutes,omitempty" jsonschema:"Relative lookback in minutes (default 60, range 1-60); omit when explicit bounds are supplied"`
	ServiceName     string   `json:"service_name,omitempty" jsonschema:"Exact canonical service filter"`
	Env             string   `json:"env,omitempty" jsonschema:"Exact canonical environment filter"`
	AlertGroupID    string   `json:"alert_group_id,omitempty" jsonschema:"Exact alert-group filter for alert episodes"`
	RuleID          string   `json:"rule_id,omitempty" jsonschema:"Exact rule filter for alert episodes"`
	EventName       string   `json:"event_name,omitempty" jsonschema:"Exact canonical change-event filter; legacy stored aliases remain readable"`
	Kinds           []string `json:"kinds,omitempty" jsonschema:"Optional event kinds: change_event and/or alert_episode; defaults to both"`
	MaxEvents       int      `json:"max_events,omitempty" jsonschema:"Maximum normalized events (default 200, range 1-500)"`
}

type timelineRequest struct {
	Args  GetChangeTimelineArgs
	Start time.Time
	End   time.Time
	Kinds []string
	Limit int
}

type timelineDependencies struct {
	client *http.Client
	cfg    models.Config
}

type followUp struct {
	Tool      string            `json:"tool"`
	Reason    string            `json:"reason"`
	Arguments map[string]string `json:"arguments"`
}

func NewGetChangeTimelineHandler(
	client *http.Client,
	cfg models.Config,
) func(context.Context, *mcp.CallToolRequest, GetChangeTimelineArgs) (*mcp.CallToolResult, any, error) {
	dependencies := timelineDependencies{client: client, cfg: cfg}
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetChangeTimelineArgs) (*mcp.CallToolResult, any, error) {
		request, err := resolveTimelineRequest(args)
		if err != nil {
			return nil, nil, err
		}
		body, err := fetchTimeline(ctx, dependencies, request)
		if err != nil {
			return nil, nil, err
		}
		output, err := addFollowUps(body, request, cfg)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Meta:    timelineMeta(cfg, request),
			Content: []mcp.Content{&mcp.TextContent{Text: string(output)}},
		}, nil, nil
	}
}

func timelineMeta(cfg models.Config, request timelineRequest) mcp.Meta {
	if !containsTimelineKind(request.Kinds, kindAlertEpisode) {
		return nil
	}
	builder := deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID)
	dashboardURL := builder.BuildAlertingLink(
		request.Start.UnixMilli(), request.End.UnixMilli(), "", request.Args.RuleID,
	)
	return deeplink.ToMeta(dashboardURL)
}

func resolveTimelineRequest(args GetChangeTimelineArgs) (timelineRequest, error) {
	args = normalizeTimelineArgs(args)
	hasStart := strings.TrimSpace(args.StartTimeISO) != ""
	hasEnd := strings.TrimSpace(args.EndTimeISO) != ""
	if hasStart != hasEnd {
		return timelineRequest{}, fmt.Errorf("start_time_iso and end_time_iso must be supplied together")
	}
	params, err := timelineTimeParams(args, hasStart)
	if err != nil {
		return timelineRequest{}, err
	}
	start, end, err := utils.GetTimeRange(params, defaultLookbackMinutes)
	if err != nil {
		return timelineRequest{}, err
	}
	if duration := end.Sub(start); duration <= 0 || duration > time.Hour {
		return timelineRequest{}, fmt.Errorf("timeline range must be greater than zero and at most 60 minutes")
	}
	kinds, err := timelineKinds(args.Kinds)
	if err != nil {
		return timelineRequest{}, err
	}
	limit, err := timelineLimit(args.MaxEvents)
	if err != nil {
		return timelineRequest{}, err
	}
	args.Kinds = kinds
	return timelineRequest{Args: args, Start: start, End: end, Kinds: kinds, Limit: limit}, nil
}

func normalizeTimelineArgs(args GetChangeTimelineArgs) GetChangeTimelineArgs {
	args.StartTimeISO = strings.TrimSpace(args.StartTimeISO)
	args.EndTimeISO = strings.TrimSpace(args.EndTimeISO)
	args.ServiceName = strings.TrimSpace(args.ServiceName)
	args.Env = strings.TrimSpace(args.Env)
	args.AlertGroupID = strings.TrimSpace(args.AlertGroupID)
	args.RuleID = strings.TrimSpace(args.RuleID)
	args.EventName = strings.TrimSpace(args.EventName)
	return args
}

func timelineTimeParams(args GetChangeTimelineArgs, hasExplicitRange bool) (map[string]interface{}, error) {
	params := map[string]interface{}{}
	if hasExplicitRange {
		params["start_time_iso"] = args.StartTimeISO
		params["end_time_iso"] = args.EndTimeISO
		return params, nil
	}
	lookback := args.LookbackMinutes
	if lookback == 0 {
		lookback = defaultLookbackMinutes
	}
	if lookback < 1 || lookback > maxLookbackMinutes {
		return nil, fmt.Errorf("lookback_minutes must be between 1 and 60")
	}
	params["lookback_minutes"] = lookback
	return params, nil
}

func timelineKinds(requested []string) ([]string, error) {
	if len(requested) == 0 {
		return []string{kindChangeEvent, kindAlertEpisode}, nil
	}
	kinds := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, rawKind := range requested {
		kind := strings.TrimSpace(rawKind)
		if _, ok := supportedKinds[kind]; !ok {
			return nil, fmt.Errorf("kinds must contain only change_event or alert_episode")
		}
		if _, duplicate := seen[kind]; !duplicate {
			kinds = append(kinds, kind)
			seen[kind] = struct{}{}
		}
	}
	return kinds, nil
}

func timelineLimit(requested int) (int, error) {
	if requested == 0 {
		return defaultMaxEvents, nil
	}
	if requested < 1 || requested > maxEvents {
		return 0, fmt.Errorf("max_events must be between 1 and 500")
	}
	return requested, nil
}

func containsTimelineKind(kinds []string, requested string) bool {
	for _, kind := range kinds {
		if kind == requested {
			return true
		}
	}
	return false
}

func fetchTimeline(
	ctx context.Context,
	dependencies timelineDependencies,
	request timelineRequest,
) ([]byte, error) {
	endpoint := dependencies.cfg.APIBaseURL + constants.EndpointChangeTimeline
	query := timelineQuery(dependencies.cfg, request).Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query, nil)
	if err != nil {
		return nil, fmt.Errorf("create change timeline request: %w", err)
	}
	httpRequest.Header.Set(constants.HeaderAccept, constants.HeaderAcceptJSON)
	httpRequest.Header.Set(constants.HeaderUserAgent, constants.UserAgentLast9MCP)
	token := dependencies.cfg.TokenManager.GetAccessToken(ctx)
	httpRequest.Header.Set(constants.HeaderXLast9APIToken, constants.BearerPrefix+token)
	response, err := dependencies.client.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("change timeline request failed: %w", ctx.Err())
		}
		return nil, fmt.Errorf("change timeline request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("change timeline API returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxTimelineResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read change timeline response: %w", err)
	}
	if len(body) > maxTimelineResponse {
		return nil, fmt.Errorf("change timeline API response exceeded %d bytes", maxTimelineResponse)
	}
	return body, nil
}

func timelineQuery(cfg models.Config, request timelineRequest) url.Values {
	query := url.Values{}
	query.Set("start_time", request.Start.UTC().Format(time.RFC3339))
	query.Set("end_time", request.End.UTC().Format(time.RFC3339))
	query.Set("limit", fmt.Sprintf("%d", request.Limit))
	setTimelineFilters(query, request.Args)
	for _, kind := range request.Kinds {
		query.Add("kind", kind)
	}
	if cfg.DatasourceName != "" {
		query.Set("data_source_name", cfg.DatasourceName)
	}
	return query
}

func setTimelineFilters(query url.Values, args GetChangeTimelineArgs) {
	filters := map[string]string{
		"service_name": args.ServiceName, "env": args.Env, "alert_group_id": args.AlertGroupID,
		"rule_id": args.RuleID, "event_name": args.EventName,
	}
	for name, value := range filters {
		if value != "" {
			query.Set(name, value)
		}
	}
}

func addFollowUps(body []byte, request timelineRequest, cfg models.Config) ([]byte, error) {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse change timeline response: %w", err)
	}
	followUps, err := json.Marshal(recommendedFollowUps(request, cfg))
	if err != nil {
		return nil, fmt.Errorf("marshal change timeline follow-ups: %w", err)
	}
	response["recommended_follow_ups"] = followUps
	output, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("marshal change timeline response: %w", err)
	}
	return output, nil
}

func recommendedFollowUps(request timelineRequest, cfg models.Config) []followUp {
	followUps := make([]followUp, 0, 4)
	if request.Args.RuleID != "" && cfg.AllowedTools.Allows("get_alert_config") {
		followUps = append(followUps, followUp{
			Tool: "get_alert_config", Reason: "Inspect the alert rule's configured intent.",
			Arguments: map[string]string{"rule_id": request.Args.RuleID},
		})
	} else if request.Args.AlertGroupID != "" && cfg.AllowedTools.Allows("get_entity_alert_rules") {
		followUps = append(followUps, followUp{
			Tool: "get_entity_alert_rules", Reason: "Inspect alert rules for the affected entity.",
			Arguments: map[string]string{"entity_id": request.Args.AlertGroupID},
		})
	}
	if request.Args.ServiceName != "" {
		for _, candidate := range serviceFollowUps(request) {
			if cfg.AllowedTools.Allows(candidate.Tool) {
				followUps = append(followUps, candidate)
			}
		}
	}
	return followUps
}

func serviceFollowUps(request timelineRequest) []followUp {
	arguments := map[string]string{
		"service_name":   request.Args.ServiceName,
		"start_time_iso": request.Start.UTC().Format(time.RFC3339),
		"end_time_iso":   request.End.UTC().Format(time.RFC3339),
	}
	if request.Args.Env != "" {
		arguments["env"] = request.Args.Env
	}
	return []followUp{
		{Tool: "get_apm_service_deviations", Reason: "Compare the selected window with its baseline.", Arguments: cloneArguments(arguments)},
		{Tool: "get_service_logs", Reason: "Corroborate the chronology with service logs.", Arguments: cloneArguments(arguments)},
		{Tool: "get_service_traces", Reason: "Corroborate the chronology with traces.", Arguments: cloneArguments(arguments)},
	}
}

func cloneArguments(arguments map[string]string) map[string]string {
	cloned := make(map[string]string, len(arguments))
	for name, value := range arguments {
		cloned[name] = value
	}
	return cloned
}
