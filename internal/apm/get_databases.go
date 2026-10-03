package apm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/databases"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetDatabasesArgs struct {
	Env             string  `json:"env,omitempty" jsonschema:"Deployment environment to filter by (e.g. production). Accepts a regular expression, e.g. prod|staging"`
	LookbackMinutes float64 `json:"lookback_minutes,omitempty" jsonschema:"Minutes to look back (default: 60, minimum: 1, maximum window: 7 days)"`
	StartTimeISO    string  `json:"start_time_iso,omitempty" jsonschema:"Start time in RFC3339 format"`
	EndTimeISO      string  `json:"end_time_iso,omitempty" jsonschema:"End time in RFC3339 format"`
}

// Metric fields are pointers so a row that never had the signal omits the key rather than
// reporting a zero a model would read as "no traffic" or "instant".
type DatabaseSummary struct {
	DBSystem       string              `json:"db_system"`
	Host           string              `json:"host"`
	Throughput     *float64            `json:"throughput_rpm,omitempty"`
	P95LatencyMs   *float64            `json:"p95_latency_ms,omitempty"`
	ErrorRate      *float64            `json:"error_rate_pct,omitempty"`
	ServiceCount   *int                `json:"service_count,omitempty"`
	ID             string              `json:"id,omitempty"`
	Env            string              `json:"env,omitempty"`
	Sources        []string            `json:"sources,omitempty"`
	Capabilities   []string            `json:"capabilities,omitempty"`
	MetricsOnly    bool                `json:"metrics_only"`
	Activity       *databases.Activity `json:"activity,omitempty"`
	ActivitySource string              `json:"activity_source,omitempty"`
	ResolvedLabels map[string]string   `json:"resolved_labels,omitempty"`
}

func NewGetDatabasesHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetDatabasesArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetDatabasesArgs) (*mcp.CallToolResult, any, error) {
		in, err := discoverInputFor(args)
		if err != nil {
			return nil, nil, err
		}

		resp, fieldErrs, err := databases.DiscoverDatabases(ctx, last9api.NewClient(client, cfg), in)
		if err != nil {
			return nil, nil, err
		}

		meta := deeplink.ToMeta(deeplink.NewBuilder(cfg.OrgSlug, cfg.ClusterID).BuildDatabasesLink())
		if len(resp.Databases) == 0 && len(fieldErrs) == 0 {
			return &mcp.CallToolResult{Meta: meta, Content: noDatabasesContent()}, nil, nil
		}

		payload, err := json.Marshal(buildPayload(resp.Databases, fieldErrs))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to marshal response: %w", err)
		}
		return &mcp.CallToolResult{
			Meta:    meta,
			Content: []mcp.Content{&mcp.TextContent{Text: string(payload)}},
		}, nil, nil
	}
}

func discoverInputFor(args GetDatabasesArgs) (databases.DiscoverDatabasesInput, error) {
	startTime, endTime, err := resolveTimeRange(args.StartTimeISO, args.EndTimeISO, args.LookbackMinutes)
	if err != nil {
		return databases.DiscoverDatabasesInput{}, err
	}
	in := databases.DiscoverDatabasesInput{
		TimeRange: databases.TimeRange{From: startTime, To: endTime},
	}
	if args.Env != "" {
		in.Filters = append(in.Filters, databases.EnvFilter(args.Env))
	}
	return in, nil
}

func noDatabasesContent() []mcp.Content {
	return []mcp.Content{&mcp.TextContent{
		Text: "No databases found for the given parameters. Databases are discovered from OpenTelemetry client spans (db_system set) and from infrastructure metrics such as CloudWatch. Widen the time range or check that either signal is being sent.",
	}}
}

// Order comes from the API so this tool and the Databases dashboard cannot drift.
func buildPayload(rows []databases.DatabaseEntity, fieldErrs []databases.FieldError) map[string]any {
	summaries := make([]DatabaseSummary, 0, len(rows))
	for _, row := range rows {
		summaries = append(summaries, toDatabaseSummary(row))
	}
	payload := map[string]any{
		"count":     len(summaries),
		"databases": summaries,
	}
	if len(fieldErrs) > 0 {
		payload["_warnings"] = warningsFrom(fieldErrs)
	}
	return payload
}

func warningsFrom(fieldErrs []databases.FieldError) []string {
	warnings := make([]string, 0, len(fieldErrs))
	for _, fe := range fieldErrs {
		warnings = append(warnings, fmt.Sprintf("%s: %s", fe.Field, fe.Reason))
	}
	return warnings
}

func toDatabaseSummary(row databases.DatabaseEntity) DatabaseSummary {
	return DatabaseSummary{
		DBSystem:       row.DBSystem,
		Host:           row.Host,
		Throughput:     row.Throughput,
		P95LatencyMs:   row.P95LatencyMs,
		ErrorRate:      row.ErrorRate,
		ServiceCount:   row.ServiceCount,
		ID:             row.ID,
		Env:            row.Env,
		Sources:        row.Sources,
		Capabilities:   row.Capabilities,
		MetricsOnly:    row.MetricsOnly,
		Activity:       row.Activity,
		ActivitySource: row.ActivitySource,
		ResolvedLabels: row.ResolvedLabels,
	}
}
