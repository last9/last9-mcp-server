package alerting

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"last9-mcp/internal/toolsets"
	"last9-mcp/internal/utils"
)

// alertIntelErrorClass buckets /alert_intelligence failures so handlers can
// tailor responder guidance instead of surfacing raw transport noise.
type alertIntelErrorClass int

const (
	classUpstream       alertIntelErrorClass = iota
	classCoverageMiss                        // chart/signal/entity not covered by the catalog
	classPermissions                         // token lacks create/read scope for intelligence
	classDuplicateName                       // rule name already exists on the group
	classInvalidRequest                      // other 400/422: the arguments were rejected
)

func (c alertIntelErrorClass) String() string {
	switch c {
	case classInvalidRequest:
		return "invalid_request"
	case classCoverageMiss:
		return "coverage_miss"
	case classPermissions:
		return "permissions"
	case classDuplicateName:
		return "duplicate_name"
	default:
		return "upstream"
	}
}

// coveredAlertSurfaces are the chart surfaces the alert-coverage catalog serves. Any other
// surface makes the API report "unknown chart key", which sends agents hunting for keys.
var coveredAlertSurfaces = []string{"discover-service", "discover-exceptions"}

// uncoveredSurfaceError returns a tool-facing message when surface is not covered, or "".
// It names create_alert only when filter serves it.
func uncoveredSurfaceError(surface string, filter toolsets.Filter) string {
	if slices.Contains(coveredAlertSurfaces, surface) {
		return ""
	}
	rule := "an alert rule"
	if filter.ServesWrite("create_alert") {
		rule = "create_alert"
	}
	return fmt.Sprintf("surface %q is not covered by chart alerts; use %s. Custom dashboard panels and other pages are not covered: bind %s to a KPI of the alert group instead",
		surface, strings.Join(coveredAlertSurfaces, " or "), rule)
}

// A 400 carrying one of these means "this chart is not alert-coverage eligible".
var alertIntelCoverageMissSentinels = []string{
	"unknown chart key",
	"unknown signal key",
	"invalid chart entity",
}

// classifyAlertIntelligenceError maps an HTTP status plus response body onto
// a stable failure class. Only 400s carrying a coverage-miss sentinel count
// as coverage misses; other 400/422s are invalid requests, never retryable.
func classifyAlertIntelligenceError(statusCode int, body string) alertIntelErrorClass {
	if statusCode == http.StatusBadRequest {
		for _, sentinel := range alertIntelCoverageMissSentinels {
			if strings.Contains(body, sentinel) {
				return classCoverageMiss
			}
		}
	}
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return classInvalidRequest
	case http.StatusUnauthorized, http.StatusForbidden:
		return classPermissions
	case http.StatusConflict:
		return classDuplicateName
	default:
		return classUpstream
	}
}

// alertIntelEntity builds the chart identity map shared by describe and
// create-from-chart: serviceName plus optional env and verbatim attributes.
// The reserved keys serviceName/env in attrs never override the seeded values.
func alertIntelEntity(serviceName, env string, attrs map[string]string) map[string]any {
	entity := map[string]any{"serviceName": serviceName}
	if env = strings.TrimSpace(env); env != "" {
		entity["env"] = env
	}
	for k, v := range attrs {
		if k == "serviceName" || k == "env" {
			continue
		}
		if strings.TrimSpace(k) != "" {
			entity[k] = v
		}
	}
	return entity
}

// alertIntelGuidance renders a classified /alert_intelligence failure as
// model-facing guidance: verbatim upstream reason plus class-specific forward
// motion. Retry framing only applies to the upstream class.
func alertIntelGuidance(e *utils.UpstreamHTTPError) string {
	var b strings.Builder
	class := classifyAlertIntelligenceError(e.StatusCode, e.Body)
	detail := e.Body
	if detail == "" {
		detail = http.StatusText(e.StatusCode)
	}
	fmt.Fprintf(&b, "alert intelligence API returned status %d (%s): %s", e.StatusCode, class, detail)
	switch class {
	case classCoverageMiss:
		b.WriteString("\n\nThis surface/chart_key/entity combination is not covered by the alert-coverage catalog. Re-run describe_alert_chart with these exact coordinates (surface, chart_key, env, attributes) to see what the catalog covers, or verify the chart against the Last9 dashboard Discover page for this service. Then pick a signal_key from that output when creating. Genuinely uncataloged charts cannot be bound by the typed MCP create flow (it attaches alerts to existing KPIs only); alert them via the Last9 dashboard or API instead.")
	case classPermissions:
		b.WriteString("\n\nThe configured credentials are not permitted on this route: alert intelligence requires a token whose role and scopes clear its POST gate; viewer-role tokens are rejected.")
	case classDuplicateName:
		b.WriteString("\n\nAn alert rule with this name already exists. Pick a different name before retrying. To inspect existing rules, use get_alert_config filtering by rule name (its output includes the entity_id), then get_entity_alert_rules with that id. After a timeout, never retry blindly — verify with get_alert_config whether the rule was actually created first, or you will create a duplicate.")
	case classInvalidRequest:
		b.WriteString("\n\nThe API rejected these arguments for the reason above; fix them and do not retry unchanged. Normally omit group_id: the API accepts only the chart's own alert group.")
	case classUpstream:
		b.WriteString("\n\nThis is an upstream server-side failure; retry once before reporting it.")
	}
	return b.String()
}
