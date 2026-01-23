package deeplink

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Route constants matching dashboard routes
const (
	RouteLogs           = "logs"
	RouteTraces         = "traces"
	RouteExceptions     = "exceptions"
	RouteServiceCatalog = "service-catalog"
	RouteGrafana        = "grafana"
	RouteAlerting       = "alerting/monitor"
	RouteChangeEvents   = "compass/changeboard"
	RouteDropRules      = "control-plane/%s/drop" // requires clusterId
)

// Builder helps construct deep links
type Builder struct {
	orgSlug string
}

// NewBuilder creates a new deep link builder for the given organization
func NewBuilder(orgSlug string) *Builder {
	return &Builder{orgSlug: orgSlug}
}

// BuildLogsLink creates a logs dashboard deep link
func (b *Builder) BuildLogsLink(fromMs, toMs int64, pipeline any) string {
	params := url.Values{}
	params.Set("from", fmt.Sprintf("%d", fromMs/1000)) // convert ms to seconds
	params.Set("to", fmt.Sprintf("%d", toMs/1000))
	if pipeline != nil {
		if pipelineJSON, err := json.Marshal(pipeline); err == nil {
			params.Set("pipeline", string(pipelineJSON))
		}
	}
	return fmt.Sprintf("/v2/organizations/%s/%s?%s", b.orgSlug, RouteLogs, params.Encode())
}

// BuildTracesLink creates a traces dashboard deep link
func (b *Builder) BuildTracesLink(fromMs, toMs int64, pipeline any, traceID, spanID string) string {
	params := url.Values{}
	params.Set("from", fmt.Sprintf("%d", fromMs/1000))
	params.Set("to", fmt.Sprintf("%d", toMs/1000))
	if pipeline != nil {
		if pipelineJSON, err := json.Marshal(pipeline); err == nil {
			params.Set("pipeline", string(pipelineJSON))
		}
	}
	if traceID != "" {
		params.Set("trace", traceID)
		params.Set("queryMode", "Trace")
	}
	if spanID != "" {
		params.Set("span", spanID)
	}
	return fmt.Sprintf("/v2/organizations/%s/%s?%s", b.orgSlug, RouteTraces, params.Encode())
}

// BuildExceptionsLink creates an exceptions dashboard deep link
func (b *Builder) BuildExceptionsLink(fromMs, toMs int64, serviceName, exceptionID string) string {
	params := url.Values{}
	params.Set("from", fmt.Sprintf("%d", fromMs/1000))
	params.Set("to", fmt.Sprintf("%d", toMs/1000))
	if serviceName != "" {
		params.Set("filter", serviceName)
	}
	if exceptionID != "" {
		params.Set("exception_id", exceptionID)
	}
	return fmt.Sprintf("/v2/organizations/%s/%s?%s", b.orgSlug, RouteExceptions, params.Encode())
}

// BuildServiceCatalogLink creates a service catalog dashboard deep link
func (b *Builder) BuildServiceCatalogLink(fromMs, toMs int64, serviceName, env, tab string) string {
	params := url.Values{}
	params.Set("from", fmt.Sprintf("%d", fromMs/1000))
	params.Set("to", fmt.Sprintf("%d", toMs/1000))
	if serviceName != "" {
		params.Set("current_service_detail", serviceName)
	}
	if env != "" && env != ".*" {
		params.Set("deployment_environment", env)
	}
	if tab != "" {
		params.Set("activeServicePanelTab", tab)
	}
	return fmt.Sprintf("/v2/organizations/%s/%s?%s", b.orgSlug, RouteServiceCatalog, params.Encode())
}

// BuildGrafanaLink creates a Grafana/metrics dashboard deep link
func (b *Builder) BuildGrafanaLink(fromMs, toMs int64) string {
	params := url.Values{}
	params.Set("from", fmt.Sprintf("%d", fromMs)) // Grafana uses milliseconds
	params.Set("to", fmt.Sprintf("%d", toMs))
	return fmt.Sprintf("/v2/organizations/%s/%s?%s", b.orgSlug, RouteGrafana, params.Encode())
}

// BuildAlertingLink creates an alerting dashboard deep link
func (b *Builder) BuildAlertingLink(fromMs, toMs int64, severity, ruleID string) string {
	params := url.Values{}
	if fromMs > 0 {
		params.Set("from", fmt.Sprintf("%d", fromMs/1000))
	}
	if toMs > 0 {
		params.Set("to", fmt.Sprintf("%d", toMs/1000))
	}
	if severity != "" {
		params.Set("severity", severity)
	}
	if ruleID != "" {
		params.Set("rule_id", ruleID)
	}
	return fmt.Sprintf("/v2/organizations/%s/%s?%s", b.orgSlug, RouteAlerting, params.Encode())
}

// BuildChangeEventsLink creates a change events dashboard deep link
func (b *Builder) BuildChangeEventsLink(fromMs, toMs int64) string {
	params := url.Values{}
	params.Set("from", fmt.Sprintf("%d", fromMs/1000))
	params.Set("to", fmt.Sprintf("%d", toMs/1000))
	return fmt.Sprintf("/v2/organizations/%s/%s?%s", b.orgSlug, RouteChangeEvents, params.Encode())
}

// BuildDropRulesLink creates a drop rules dashboard deep link
func (b *Builder) BuildDropRulesLink(clusterID string) string {
	route := fmt.Sprintf(RouteDropRules, clusterID)
	return fmt.Sprintf("/v2/organizations/%s/%s", b.orgSlug, route)
}

// ToMeta converts a dashboard URL to MCP Meta format
func ToMeta(dashboardURL string) mcp.Meta {
	return mcp.Meta{
		"reference_url": dashboardURL,
	}
}
