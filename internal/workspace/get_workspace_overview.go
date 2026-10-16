package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"last9-mcp/internal/alerting"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/alerts"
	apilogs "last9-mcp/internal/last9api/logs"
	"last9-mcp/internal/last9api/prom"
	apitraces "last9-mcp/internal/last9api/traces"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// overviewLookback is the window telemetry presence and the APM label probes
// read. An hour keeps every probe cheap while still seeing idle services.
const overviewLookback = time.Hour

// overviewCallTimeout bounds each sub-call so one slow backend costs a null
// field, not the whole first call.
const overviewCallTimeout = 10 * time.Second

// firingAlertsWindow matches get_alerts' default window.
const firingAlertsWindow = 15 * time.Minute

const apmServerSpans = "domain_attributes_count{span_kind='SPAN_KIND_SERVER'}"

type GetWorkspaceOverviewArgs struct{}

// Telemetry reports which signals were seen in the lookback; nil means the
// probe failed.
type Telemetry struct {
	Logs    *bool `json:"logs"`
	Traces  *bool `json:"traces"`
	Metrics *bool `json:"metrics"`
}

type OverviewError struct {
	Field string `json:"field"`
	Error string `json:"error"`
}

// WorkspaceOverview leaves a field null when its sub-call failed and names
// the failure in Errors.
type WorkspaceOverview struct {
	CurrentTime      CurrentTime     `json:"current_time"`
	LookbackMinutes  int             `json:"lookback_minutes"`
	Environments     []string        `json:"environments"`
	Telemetry        Telemetry       `json:"telemetry"`
	ServiceCount     *int            `json:"service_count"`
	AlertGroupCount  *int            `json:"alert_group_count"`
	FiringAlertCount *int            `json:"firing_alert_count"`
	Errors           []OverviewError `json:"errors"`
}

type overviewOptions struct {
	now         func() time.Time
	callTimeout time.Duration
}

func NewGetWorkspaceOverviewHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, GetWorkspaceOverviewArgs) (*mcp.CallToolResult, any, error) {
	return newWorkspaceOverviewHandler(client, cfg, overviewOptions{now: time.Now, callTimeout: overviewCallTimeout})
}

func newWorkspaceOverviewHandler(client *http.Client, cfg models.Config, opts overviewOptions) func(context.Context, *mcp.CallToolRequest, GetWorkspaceOverviewArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ GetWorkspaceOverviewArgs) (*mcp.CallToolResult, any, error) {
		now := opts.now()
		current, err := resolveCurrentTime(now, "", "")
		if err != nil {
			return nil, nil, err
		}
		p := overviewProbe{
			api:     last9api.NewClient(client, cfg),
			http:    client,
			cfg:     cfg,
			now:     now,
			timeout: opts.callTimeout,
		}
		body, err := json.Marshal(p.run(ctx, current))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to encode workspace overview: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
		}, nil, nil
	}
}

type overviewProbe struct {
	api     *last9api.Client
	http    *http.Client
	cfg     models.Config
	now     time.Time
	timeout time.Duration
}

// result is one sub-call's outcome; run assembles them in a fixed order so
// errors[] reads the same on every call.
type result[T any] struct {
	value T
	err   error
}

func (p overviewProbe) run(ctx context.Context, current CurrentTime) WorkspaceOverview {
	var (
		envs, services        result[[]string]
		logs, traces, metrics result[bool]
		groups, firing        result[int]
		wg                    sync.WaitGroup
	)
	countGroups := func(ctx context.Context) (int, error) { return alerting.CountAlertGroups(ctx, p.http, p.cfg) }
	wg.Go(func() { envs = withTimeout(ctx, p.timeout, p.apmLabelValues("env")) })
	wg.Go(func() { services = withTimeout(ctx, p.timeout, p.apmLabelValues("service_name")) })
	wg.Go(func() { logs = withTimeout(ctx, p.timeout, p.hasLogs) })
	wg.Go(func() { traces = withTimeout(ctx, p.timeout, p.hasTraces) })
	wg.Go(func() { metrics = withTimeout(ctx, p.timeout, p.hasMetrics) })
	wg.Go(func() { groups = withTimeout(ctx, p.timeout, countGroups) })
	wg.Go(func() { firing = withTimeout(ctx, p.timeout, p.firingAlerts) })
	wg.Wait()

	out := WorkspaceOverview{
		CurrentTime:     current,
		LookbackMinutes: int(overviewLookback / time.Minute),
		Errors:          []OverviewError{},
	}
	note := func(field string, err error) bool {
		if err != nil {
			out.Errors = append(out.Errors, OverviewError{Field: field, Error: err.Error()})
		}
		return err == nil
	}
	if note("environments", envs.err) {
		out.Environments = nonNil(envs.value)
		slices.Sort(out.Environments)
	}
	if note("service_count", services.err) {
		out.ServiceCount = ptr(len(services.value))
	}
	if note("telemetry.logs", logs.err) {
		out.Telemetry.Logs = ptr(logs.value)
	}
	if note("telemetry.traces", traces.err) {
		out.Telemetry.Traces = ptr(traces.value)
	}
	if note("telemetry.metrics", metrics.err) {
		out.Telemetry.Metrics = ptr(metrics.value)
	}
	if note("alert_group_count", groups.err) {
		out.AlertGroupCount = ptr(groups.value)
	}
	if note("firing_alert_count", firing.err) {
		out.FiringAlertCount = ptr(firing.value)
	}
	return out
}

func withTimeout[T any](ctx context.Context, timeout time.Duration, f func(context.Context) (T, error)) result[T] {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	v, err := f(ctx)
	return result[T]{v, err}
}

func (p overviewProbe) datasource() prom.Datasource {
	return prom.Datasource{ReadURL: p.cfg.PrometheusReadURL, Username: p.cfg.PrometheusUsername, Password: p.cfg.PrometheusPassword}
}

func (p overviewProbe) apmLabelValues(label string) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		return prom.GetLabelValues(ctx, p.api, prom.GetLabelValuesInput{
			Datasource: p.datasource(),
			Label:      label,
			Matches:    []string{apmServerSpans},
			Timestamp:  p.now.Unix(),
			Window:     int64(overviewLookback / time.Second),
		})
	}
}

func (p overviewProbe) hasMetrics(ctx context.Context) (bool, error) {
	names, err := prom.GetLabelValues(ctx, p.api, prom.GetLabelValuesInput{
		Datasource: p.datasource(),
		Label:      "__name__",
		Timestamp:  p.now.Unix(),
		Window:     int64(overviewLookback / time.Second),
		Limit:      1,
	})
	return len(names) > 0, err
}

func (p overviewProbe) hasLogs(ctx context.Context) (bool, error) {
	labels, err := apilogs.ListLabels(ctx, p.api, apilogs.ListLabelsInput{Start: p.now.Add(-overviewLookback).Unix(), End: p.now.Unix()})
	return len(labels) > 0, err
}

func (p overviewProbe) hasTraces(ctx context.Context) (bool, error) {
	resp, err := apitraces.ListSearchTags(ctx, p.api, apitraces.ListSearchTagsInput{Start: p.now.Add(-overviewLookback).Unix(), End: p.now.Unix()})
	if err != nil {
		return false, err
	}
	for _, scope := range resp.Scopes {
		for _, tag := range scope.Tags {
			if tag != "" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (p overviewProbe) firingAlerts(ctx context.Context) (int, error) {
	resp, err := alerts.GetMonitor(ctx, p.api, alerts.GetMonitorInput{Timestamp: p.now.Unix(), Window: int64(firingAlertsWindow / time.Second)})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, rule := range resp.AlertRules {
		for _, alert := range rule.Alerts {
			if strings.EqualFold(alert.State, "firing") {
				n++
			}
		}
	}
	return n, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func ptr[T any](v T) *T { return &v }
