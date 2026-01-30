package deeplink

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Route constants matching dashboard routes
const (
	RouteLogs                = "logs"
	RouteTraces              = "traces"
	RouteExceptions          = "exceptions"
	RouteServiceCatalog      = "service-catalog"
	RouteAlerting            = "alerting/monitor"
	RouteAlertingGroups      = "alerting/groups"
	RouteDropRules           = "control-plane/%s/drop" // requires clusterId
	RouteCompassEntityHealth = "compass/entities/%s/health"
)

// Builder helps construct deep links
type Builder struct {
	orgSlug   string
	clusterID string
}

// NewBuilder creates a new deep link builder for the given organization and cluster
func NewBuilder(orgSlug, clusterID string) *Builder {
	return &Builder{orgSlug: orgSlug, clusterID: clusterID}
}

// BuildLogsLink creates a logs dashboard deep link
func (b *Builder) BuildLogsLink(fromMs, toMs int64, pipeline any) string {
	params := url.Values{}
	params.Set("from", fmt.Sprintf("%d", fromMs/1000)) // convert ms to seconds
	params.Set("to", fmt.Sprintf("%d", toMs/1000))
	if b.clusterID != "" {
		params.Set("cluster", b.clusterID)
	}
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
	if b.clusterID != "" {
		params.Set("cluster", b.clusterID)
	}
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
	if b.clusterID != "" {
		params.Set("cluster", b.clusterID)
	}
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
	if b.clusterID != "" {
		params.Set("cluster", b.clusterID)
	}
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

// BuildDropRulesLink creates a drop rules dashboard deep link
func (b *Builder) BuildDropRulesLink() string {
	clusterID := b.clusterID
	if clusterID == "" {
		clusterID = "default"
	}
	route := fmt.Sprintf(RouteDropRules, clusterID)
	return fmt.Sprintf("/v2/organizations/%s/%s", b.orgSlug, route)
}

// BuildCompassEntityHealthLink creates a compass entity health page deep link
func (b *Builder) BuildCompassEntityHealthLink(entityID, ruleName string) string {
	route := fmt.Sprintf(RouteCompassEntityHealth, entityID)
	params := url.Values{}
	if ruleName != "" {
		params.Set("rule_name", ruleName)
	}
	if len(params) > 0 {
		return fmt.Sprintf("/v2/organizations/%s/%s?%s", b.orgSlug, route, params.Encode())
	}
	return fmt.Sprintf("/v2/organizations/%s/%s", b.orgSlug, route)
}

// BuildAPMServiceLink creates a deep link to the APM service catalog page with the service name in the path
// and environment filter as a JSON array
func (b *Builder) BuildAPMServiceLink(fromMs, toMs int64, serviceName, env, tab string) string {
	params := url.Values{}
	params.Set("live", "true")
	params.Set("from", fmt.Sprintf("%d", fromMs/1000))
	params.Set("to", fmt.Sprintf("%d", toMs/1000))
	if b.clusterID != "" {
		params.Set("cluster", b.clusterID)
	}
	if env != "" && env != ".*" {
		// Format: ["deployment_environment=\"{env}\""]
		filterValue := fmt.Sprintf(`["deployment_environment=\"%s\""]`, env)
		params.Set("filter", filterValue)
	}
	if tab != "" {
		params.Set("activeServicePanelTab", tab)
	}
	if serviceName != "" {
		return fmt.Sprintf("/v2/organizations/%s/%s/%s?%s", b.orgSlug, RouteServiceCatalog, serviceName, params.Encode())
	}
	return fmt.Sprintf("/v2/organizations/%s/%s?%s", b.orgSlug, RouteServiceCatalog, params.Encode())
}

// BuildAlertingGroupsLink creates a deep link to the alerting groups page
func (b *Builder) BuildAlertingGroupsLink() string {
	return fmt.Sprintf("/v2/organizations/%s/%s", b.orgSlug, RouteAlertingGroups)
}

// ToMeta converts a dashboard URL to MCP Meta format
func ToMeta(dashboardURL string) mcp.Meta {
	return mcp.Meta{
		"reference_url": dashboardURL,
	}
}
