package constants

// API Endpoints
const (
	// Traces API endpoints
	EndpointTracesQueryRange = "/cat/api/traces/v2/query_range/json"
	EndpointTracesSeries     = "/cat/api/traces/v2/series/json"

	// Logs API endpoints
	EndpointLogsQueryRange = "/logs/api/v2/query_range/json"

	// Prometheus API endpoints
	EndpointPromQueryInstant = "/prom_query_instant"
	EndpointPromQuery        = "/prom_query"
	EndpointPromLabelValues  = "/prom_label_values"
	EndpointPromLabels       = "/prom_labels"
	EndpointAPMLabels        = "/apm/labels"

	// Organization and configuration endpoints
	EndpointDatasources         = "/datasources"
	EndpointOAuthAccessToken    = "/api/v4/oauth/access_token"
	EndpointLogsSettingsRouting = "/api/v4/organizations/%s/logs_settings/routing"
	EndpointAlertRules          = "/alert-rules"
	EndpointAlertsMonitor       = "/alerts/monitor"

	// Discovery API endpoints (appended to APIBaseURL which includes /api/v4/organizations/{org})
	EndpointDiscoverSystem  = "/knowledge_graph/system/context"
	EndpointDiscoverMetrics = "/knowledge_graph/metric/context"

	// API Base URL
	APIBaseHost = "app.last9.io"
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
