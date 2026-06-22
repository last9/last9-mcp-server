package constants

import "time"

// API Endpoints
const (
	// Traces API endpoints
	EndpointTracesQueryRange = "/cat/api/traces/v2/query_range/json"
	EndpointTracesSeries     = "/cat/api/traces/v2/series/json"
	EndpointTraceDetails     = "/cat/api/traces/%s"
	EndpointTraceTagValues   = "/cat/api/traces/v2/label/json/%s/values"
	// EndpointTraceTags is the global trace tag catalog (scopes with prefixes stripped).
	EndpointTraceTags = "/cat/api/search/tags"

	// Logs API endpoints
	EndpointLogsQueryRange = "/logs/api/v2/query_range/json"
	// EndpointLogsSeries returns the label-sets present for a given log pipeline.
	// Used for pipeline-scoped attribute discovery.
	EndpointLogsSeries = "/logs/api/v2/series/json"

	// Prometheus API endpoints
	EndpointPromQueryInstant = "/prom_query_instant"
	EndpointPromQuery        = "/prom_query"
	EndpointPromLabelValues  = "/prom_label_values"
	EndpointPromLabels       = "/prom_labels"
	EndpointAPMLabels        = "/apm/labels"

	// Organization and configuration endpoints
	EndpointDatasources          = "/datasources"
	EndpointOAuthAccessToken     = "/api/v4/oauth/access_token"
	EndpointLogsSettingsRouting  = "/logs_settings/routing"
	EndpointAlertRules           = "/alert-rules"
	EndpointAlertsMonitor        = "/alerts/monitor"
	EndpointEntitiesList         = "/entities/list"
	EndpointEntityKPI            = "/entities/%s/kpis/%s"
	EndpointEntityAlertRules     = "/entities/%s/alert-rules"
	EndpointNotificationSettings = "/notification_settings"
	// EndpointSuggest returns fuzzy entity-name suggestions for the did_you_mean tool.
	EndpointSuggest = "/suggest"

	// Dashboard API endpoints (v4)
	EndpointDashboards    = "/dashboards"
	EndpointDashboardByID = "/dashboards/%s" // fmt with id; GET requires ?region=

	// DefaultHTTPTimeout is the fixed timeout used for outbound API calls and HTTP server read/write operations.
	DefaultHTTPTimeout = 3 * time.Minute

	// PerChunkHTTPTimeout bounds a single chunked upstream call so one slow
	// chunk can't stall the whole tool invocation. ENG-914.
	PerChunkHTTPTimeout = 30 * time.Second
)

// HTTP Headers
const (
	HeaderAccept          = "Accept"
	HeaderContentType     = "Content-Type"
	HeaderXLast9APIToken  = "X-LAST9-API-TOKEN"
	HeaderUserAgent       = "User-Agent"
	HeaderContentTypeJSON = "application/json"
	HeaderAcceptJSON      = "application/json"
)

// Bearer token prefix
const BearerPrefix = "Bearer "

// User Agent
const UserAgentLast9MCP = "Last9-MCP-Server/1.0"
