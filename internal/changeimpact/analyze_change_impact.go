package changeimpact

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"last9-mcp/internal/change_events"
	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AnalyzeChangeImpactArgs struct {
	ChangeEvent     *change_events.TimeSeries `json:"change_event,omitempty" jsonschema:"One element of change_events[] from get_change_events, passed unchanged. Its metric.service, metric.deployment_environment and earliest values[].timestamp are used. Do not combine with service or timestamp."`
	Service         string                    `json:"service,omitempty" jsonschema:"Service that changed. Required with timestamp when change_event is omitted."`
	Timestamp       string                    `json:"timestamp,omitempty" jsonschema:"Change time: RFC3339 (2026-10-16T12:00:00Z) or unix seconds as a string. Required with service when change_event is omitted."`
	Env             string                    `json:"env,omitempty" jsonschema:"Exact environment. Defaults to the change_event's deployment_environment, else all environments."`
	BaselineMinutes int                       `json:"baseline_minutes,omitempty" jsonschema:"Minutes before the change to compare against (default 60, clamped to 30..120)."`
	AfterMinutes    int                       `json:"after_minutes,omitempty" jsonschema:"Minutes after the change to examine (default baseline_minutes, clamped to 30..120, capped at now)."`
}

func NewAnalyzeChangeImpactHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AnalyzeChangeImpactArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args AnalyzeChangeImpactArgs) (*mcp.CallToolResult, any, error) {
		req, err := requestFromArgs(args)
		if err != nil {
			return nil, nil, err
		}
		report := Analyze(ctx, client, cfg, req)
		payload, err := json.Marshal(report)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal change impact report: %w", err)
		}
		return &mcp.CallToolResult{
			Meta:    deeplink.ToMeta(report.DashboardURL),
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, nil, nil
	}
}

func requestFromArgs(args AnalyzeChangeImpactArgs) (Request, error) {
	service := strings.TrimSpace(args.Service)
	timestamp := strings.TrimSpace(args.Timestamp)
	req := Request{
		Env:             strings.TrimSpace(args.Env),
		BaselineMinutes: args.BaselineMinutes,
		AfterMinutes:    args.AfterMinutes,
	}

	if args.ChangeEvent != nil {
		if service != "" || timestamp != "" {
			return Request{}, fmt.Errorf("pass change_event or service and timestamp, not both")
		}
		event := args.ChangeEvent
		req.Service = strings.TrimSpace(event.Metric["service"])
		if req.Service == "" {
			return Request{}, fmt.Errorf("change_event has no metric.service; pass service and timestamp instead")
		}
		if len(event.Values) == 0 {
			return Request{}, fmt.Errorf("change_event has no values, so its time is unknown; pass service and timestamp instead")
		}
		earliest := event.Values[0].Timestamp
		for _, p := range event.Values[1:] {
			earliest = min(earliest, p.Timestamp)
		}
		req.Change = time.Unix(int64(earliest), 0).UTC()
		if req.Env == "" {
			req.Env = strings.TrimSpace(event.Metric["deployment_environment"])
		}
		return req, nil
	}

	if service == "" {
		return Request{}, fmt.Errorf("analyze_change_impact needs change_event or service and timestamp")
	}
	if timestamp == "" {
		return Request{}, fmt.Errorf("timestamp is required with service")
	}
	change, err := parseChangeTimestamp(timestamp)
	if err != nil {
		return Request{}, err
	}
	req.Service = service
	req.Change = change
	return req, nil
}

// Above this a digit-only timestamp is milliseconds or finer, not seconds.
const maxUnixSeconds = 1e11

func parseChangeTimestamp(value string) (time.Time, error) {
	if secs, err := strconv.ParseInt(value, 10, 64); err == nil {
		if secs <= 0 || secs >= maxUnixSeconds {
			return time.Time{}, fmt.Errorf("timestamp %q looks like milliseconds or is out of range; pass unix seconds or RFC3339", value)
		}
		return time.Unix(secs, 0).UTC(), nil
	}
	t, err := utils.ParseToolTimestamp(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp: %w", err)
	}
	return t, nil
}
