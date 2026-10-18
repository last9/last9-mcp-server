package prompts_test

import (
	"os"
	"sort"
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

var promptDescriptionFiles = map[string]string{
	"add_drop_rule.md":                     prompts.AddDropRuleDescription,
	"create_alert.md":                      prompts.CreateAlertDescription,
	"create_alert_from_chart.md":           prompts.CreateAlertFromChartDescription,
	"create_dashboard.md":                  prompts.CreateDashboardDescription,
	"delete_alert.md":                      prompts.DeleteAlertDescription,
	"delete_dashboard.md":                  prompts.DeleteDashboardDescription,
	"delete_dashboard_snapshot.md":         prompts.DeleteDashboardSnapshotDescription,
	"describe_alert_chart.md":              prompts.DescribeAlertChartDescription,
	"did_you_mean.md":                      prompts.DidYouMeanDescription,
	"get_alert_config.md":                  prompts.GetAlertConfigDescription,
	"get_alert_groups.md":                  prompts.GetAlertGroupsDescription,
	"get_alert_rule_state.md":              prompts.GetAlertRuleStateDescription,
	"get_alerts.md":                        prompts.GetAlertsDescription,
	"get_apm_service_deviations.md":        prompts.GetAPMServiceDeviationsDescription,
	"get_change_events.md":                 prompts.GetChangeEventsDescription,
	"get_current_time.md":                  prompts.GetCurrentTimeDescription,
	"get_dashboard.md":                     prompts.GetDashboardDescription,
	"get_dashboard_snapshot.md":            prompts.GetDashboardSnapshotDescription,
	"get_database_queries.md":              prompts.GetDatabaseQueriesDescription,
	"get_database_server_metrics.md":       prompts.GetDatabaseServerMetricsDescription,
	"get_database_slow_queries.md":         prompts.GetDatabaseSlowQueriesDescription,
	"get_databases.md":                     prompts.GetDatabasesDescription,
	"get_drop_rules.md":                    prompts.GetDropRulesDescription,
	"get_entity_alert_rules.md":            prompts.GetEntityAlertRulesDescription,
	"get_exceptions.md":                    prompts.GetExceptionsInstructions,
	"get_flamegraph.md":                    prompts.GetFlamegraphDescription,
	"get_log_attributes.md":                prompts.GetLogAttributesDescription,
	"get_log_attributes_for_pipeline.md":   prompts.GetLogAttributesForPipelineDescription,
	"get_logs_base.md":                     prompts.GetLogsDescription,
	"get_metric_status.md":                 prompts.GetMetricStatusDescription,
	"get_notification_channels.md":         prompts.GetNotificationChannelsDescription,
	"get_profile_services.md":              prompts.GetProfileServicesDescription,
	"get_profile_summary.md":               prompts.GetProfileSummaryDescription,
	"get_service_dependency_graph.md":      prompts.GetServiceDependencyGraphDetails,
	"get_service_environments.md":          prompts.GetServiceEnvironmentsDescription,
	"get_service_logs_base.md":             prompts.GetServiceLogsDescription,
	"get_service_operations_summary.md":    prompts.GetServiceOperationsSummaryDescription,
	"get_service_performance_details.md":   prompts.GetServicePerformanceDetails,
	"get_service_profile.md":               prompts.GetServiceProfileDescription,
	"get_service_summary.md":               prompts.GetServiceSummaryDescription,
	"get_service_traces.md":                prompts.GetServiceTracesDescription,
	"get_top_functions.md":                 prompts.GetTopFunctionsDescription,
	"get_trace_attribute_deviations.md":    prompts.GetTraceAttributeDeviationsDescription,
	"get_trace_attribute_values.md":        prompts.GetTraceAttributeValuesDescription,
	"get_trace_attributes.md":              prompts.GetTraceAttributesDescription,
	"get_trace_attributes_for_pipeline.md": prompts.GetTraceAttributesForPipelineDescription,
	"get_trace_waterfall.md":               prompts.GetTraceWaterfallDescription,
	"get_workspace_overview.md":            prompts.GetWorkspaceOverviewDescription,
	"get_traces_base.md":                   prompts.GetTracesDescription,
	"grafana_get_dashboard.md":             prompts.GrafanaGetDashboardDescription,
	"grafana_list_datasources.md":          prompts.GrafanaListDatasourcesDescription,
	"grafana_list_folder_dashboards.md":    prompts.GrafanaListFolderDashboardsDescription,
	"grafana_list_folders.md":              prompts.GrafanaListFoldersDescription,
	"grafana_search_dashboards.md":         prompts.GrafanaSearchDashboardsDescription,
	"list_dashboard_snapshots.md":          prompts.ListDashboardSnapshotsDescription,
	"list_dashboards.md":                   prompts.ListDashboardsDescription,
	"list_datasources.md":                  prompts.ListDatasourcesDescription,
	"mute_alert_group.md":                  prompts.MuteAlertGroupDescription,
	"acknowledge_alert.md":                 prompts.AcknowledgeAlertDescription,
	"reassign_alert.md":                    prompts.ReassignAlertDescription,
	"create_notification_channel.md":       prompts.CreateNotificationChannelDescription,
	"attach_notification_channel.md":       prompts.AttachNotificationChannelDescription,
	"detach_notification_channel.md":       prompts.DetachNotificationChannelDescription,
	"delete_notification_channel.md":       prompts.DeleteNotificationChannelDescription,
	"patch_alert.md":                       prompts.PatchAlertDescription,
	"prometheus_instant_query.md":          prompts.PromqlInstantQueryDetails,
	"prometheus_label_values.md":           prompts.PromqlLabelValuesQueryDetails,
	"prometheus_labels.md":                 prompts.PromqlLabelsQueryDetails,
	"prometheus_range_query_base.md":       prompts.PromqlRangeQueryDetails,
	"recommend_alert_config.md":            prompts.RecommendAlertConfigDescription,
	"update_alert.md":                      prompts.UpdateAlertDescription,
	"update_dashboard.md":                  prompts.UpdateDashboardDescription,
	"validate_alert_expression.md":         prompts.ValidateAlertExpressionDescription,
	"validate_dashboard.md":                prompts.ValidateDashboardDescription,
}

var simplePromptDescriptionFiles = map[string]bool{
	"create_alert.md":              true,
	"delete_alert.md":              true,
	"patch_alert.md":               true,
	"recommend_alert_config.md":    true,
	"update_alert.md":              true,
	"add_drop_rule.md":             true,
	"create_dashboard.md":          true,
	"delete_dashboard.md":          true,
	"delete_dashboard_snapshot.md": true,
	"get_dashboard.md":             true,
	"get_dashboard_snapshot.md":    true,
	"get_drop_rules.md":            true,
	"get_notification_channels.md": true,
	"list_dashboard_snapshots.md":  true,
	"list_dashboards.md":           true,
	"list_datasources.md":          true,
	"update_dashboard.md":          true,
	"validate_dashboard.md":        true,
}

func TestPromptDescriptionFilesLoadCleanMarkdown(t *testing.T) {
	for name, body := range promptDescriptionFiles {
		t.Run(name, func(t *testing.T) {
			description := strings.TrimSpace(body)
			if description == "" {
				t.Fatal("embedded description is empty")
			}
			if strings.Contains(description, "{{") {
				t.Fatalf("embedded description contains unresolved template syntax: %q", snippet(description))
			}
			if strings.Contains(description, "<no value>") {
				t.Fatal("embedded description contains a rendered empty template value")
			}
			if !hasDescriptionStructure(name, description) {
				t.Fatalf("embedded description lacks markdown structure and is not an allowlisted simple description: %q", snippet(description))
			}
			base := genericBaseDescriptionForFile(name)
			if len(description) <= len(base) {
				t.Fatalf("embedded description length %d must be greater than generic base length %d", len(description), len(base))
			}
		})
	}
}

func TestPromptDescriptionEmbedsMatchMarkdownFiles(t *testing.T) {
	for name, body := range promptDescriptionFiles {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("descriptions/" + name)
			if err != nil {
				t.Fatal(err)
			}
			if body != string(raw) {
				t.Fatal("embedded description does not match markdown file bytes")
			}
		})
	}
}

func TestPromptDescriptionRegistryMatchesDirectory(t *testing.T) {
	files, err := os.ReadDir("descriptions")
	if err != nil {
		t.Fatal(err)
	}

	want := make(map[string]bool)
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".md") {
			continue
		}
		want[file.Name()] = true
	}

	var missing []string
	for name := range want {
		if _, ok := promptDescriptionFiles[name]; !ok {
			missing = append(missing, name)
		}
	}
	var extra []string
	for name := range promptDescriptionFiles {
		if !want[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) != 0 {
		t.Errorf("description files missing from promptDescriptionFiles: %v", missing)
	}
	if len(extra) != 0 {
		t.Errorf("promptDescriptionFiles entries without matching markdown files: %v", extra)
	}
}

func hasDescriptionStructure(name, description string) bool {
	structuralMarkers := []string{
		"\n- ",
		"\n* ",
		"\n1. ",
		"## ",
		"`",
		"Parameters:",
		"Required parameters:",
		"Optional filters",
		"Examples:",
		"Response:",
		"Returns:",
	}
	for _, marker := range structuralMarkers {
		if strings.Contains(description, marker) {
			return true
		}
	}
	return simplePromptDescriptionFiles[name] &&
		strings.Count(description, ".") > 0 &&
		len(description) >= 40 &&
		len(strings.Fields(description)) >= 6
}

func genericBaseDescriptionForFile(name string) string {
	stem := strings.TrimSuffix(name, ".md")
	stem = strings.TrimSuffix(stem, "_base")
	words := strings.Split(stem, "_")
	if len(words) == 0 {
		return "."
	}
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ") + "."
}

func snippet(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= 120 {
		return s
	}
	return s[:120] + "..."
}

func TestGetLogsDescriptionCriticalRules(t *testing.T) {
	desc := prompts.GetLogsDescription
	if desc == "" {
		t.Fatal("GetLogsDescription is empty — embed directive missing")
	}
	checks := []struct {
		phrase string
		reason string
	}{
		{"window_aggregate", "must document time-bucketed counts"},
		{"window", "must document window key for window_aggregate"},
		{"function", "must document function key for window_aggregate"},
		{"\"function\":{\"$count\":[]},\"as\":\"count\",\"window\":[\"5\",\"minutes\"]", "must show a generic windowed count"},
		{"\"type\":\"window_aggregate\",\"function\":{\"$count\":[]}", "must include the required stage type in the windowed count example"},
		{"Ops: `$and`/`$or`/`$not`", "must retain a useful filter operator list"},
		{"`$containsWords` args `[field,word]` (e.g. `[\"Body\",\"timeout\"]`)", "must show the required field argument for body-word filters"},
		{"NOT SQL", "must clarify logjson_query is not SQL"},
		{"$neq", "must document existence idiom"},
		{"`$neq` (is not X; never `$not`+`$eq`)", "must route negated equality to $neq"},
		{"start_time_iso", "must document absolute time on tool args"},
		{"resources['last9.tenant']", "must document tenant scoping"},
		{"resources['deployment.environment']", "must document env scoping"},
		{"no `service_name` arg; filter ServiceName in query", "must place service scope in the get_logs pipeline"},
		{"last9://reference/logjson", "must point at the logjson resource"},
		{"Predicates: one standalone, many in `$and`", "must allow valid standalone predicates"},
		{"high→add filter, recount", "must repeat counts after narrowing broad filters"},
		{"community_member_id", "must document bare single-token field mistake"},
		{"attributes['key']", "must require attributes/resources wrapping"},
		{"Parse Body-derived fields before filtering, grouping, or aggregation", "must parse Body fields before grouping or aggregation"},
		{"`$quantile` is the general/default percentile operator", "must default to approximate percentile aggregation"},
		{"\"function\":{\"$quantile\":[0.99,\"attributes['latency_ms']\"]},\"as\":\"p99\",\"window\":[\"24\",\"hours\"]", "must show a complete day-wise percentile window"},
		{"\"type\":\"window_aggregate\",\"function\":{\"$quantile\":[0.99,\"attributes['latency_ms']\"]}", "must include the required stage type in the windowed percentile example"},
		{"Day-wise: exactly ONE get_logs call over the full half-open start_time_iso/end_time_iso range with one window_aggregate; NEVER one call per day; honor requested timezone.", "must require one timezone-aware full-range day-wise call"},
		{"Never template/merge/recombine aggregated percentile rows", "must forbid client-side percentile reconstruction"},
		{"use the canonical anchored numeric `$regex` shown", "must require the deliberate canonical numeric allowlist"},
		{"excludes non-matching values from percentile calculations; disclose that exclusion in the answer", "must disclose the numeric filter's effect"},
		{"Use a discovered normalized route", "must discover normalized route semantics before aggregation"},
		{"Raw URI: aggregate exact values; never normalize/merge later", "must allow exact raw URI aggregation without unsafe normalization"},
		{"If `l9_result.partial=true`, preserve rows and disclose partial coverage", "must tie partial disclosure to result metadata"},
		{"Units: report source; never convert", "must prevent inferred latency units"},
		{"Only report returned P99 values; empty result→say unavailable and stop", "must prevent fallback counts from replacing an empty percentile result"},
		{"ALL→`$and`; ANY→`$or`", "must document all/any-word Body searches"},
		{"never `$icontainsWords`", "must avoid the slower case-insensitive word operator"},
		{"no service→search `Body` with `$contains` (even service-like)", "must distinguish standalone identifiers from service names"},
	}
	for _, c := range checks {
		if !strings.Contains(desc, c.phrase) {
			t.Errorf("GetLogsDescription missing %q: %s", c.phrase, c.reason)
		}
	}
	if strings.Contains(desc, "window_minutes") {
		t.Error("GetLogsDescription must NOT contain deprecated 'window_minutes' key")
	}
	if !strings.Contains(desc, `[{"type":"filter","query":{"$and":[{"$eq":["SeverityText","ERROR"]}]}}]`) {
		t.Error("GetLogsDescription must include its canonical JSON stage-array example")
	}
}

func TestGetTracesDescriptionCriticalRules(t *testing.T) {
	desc := prompts.GetTracesDescription
	if desc == "" {
		t.Fatal("GetTracesDescription is empty — embed directive missing")
	}
	checks := []struct {
		phrase string
		reason string
	}{
		{"$regex", "must document pattern match operator"},
		{"NOT SQL", "must clarify tracejson_query is not SQL"},
		{"$neq", "must document existence idiom"},
		{"aggregates", "must document aggregate key name"},
		{"groupby", "must document groupby key name"},
		{"resources['last9.tenant']", "must document tenant scoping"},
		{"`service.name`→`ServiceName`", "must map service.name to the canonical service field"},
		{"last9://reference/tracejson", "must point at the tracejson resource"},
		{"default **60**", "must document default lookback of 60 minutes"},
		{"`$quantile` is the general/default percentile operator", "must default to approximate percentile aggregation"},
		{"Compute from raw spans; never average percentile samples", "must document non-composable percentile semantics"},
		{"`Duration` is numeric already; for `attributes[...]` percentiles, `$regex`-gate numeric values first", "must gate attribute values without gating Duration"},
		{"For calendar buckets, use explicit ISO bounds and time zone", "must make calendar boundaries explicit"},
		{"P99 `Duration` output remains nanoseconds", "must preserve Duration units"},
	}
	for _, c := range checks {
		if !strings.Contains(desc, c.phrase) {
			t.Errorf("GetTracesDescription missing %q: %s", c.phrase, c.reason)
		}
	}
	if !strings.Contains(desc, `[{"type":"filter","query":{"$and":[{"$eq":["StatusCode","STATUS_CODE_ERROR"]}]}}]`) {
		t.Error("GetTracesDescription must include its canonical JSON stage-array example")
	}
}

func TestPrometheusQueryDescriptionsUseValidPercentileSources(t *testing.T) {
	for name, desc := range map[string]string{
		"instant": prompts.PromqlInstantQueryDetails,
		"range":   prompts.PromqlRangeQueryDetails,
	} {
		for _, phrase := range []string{
			"Percentiles are not composable: never average precomputed percentile series",
			"suitable Prometheus histogram buckets",
			"histogram_quantile(..., rate(...)) or histogram_quantile(..., increase(...))",
			"Otherwise use get_logs/get_traces to aggregate raw values",
			"recorded window exactly matches the requested window",
		} {
			if !strings.Contains(desc, phrase) {
				t.Errorf("%s Prometheus description missing %q", name, phrase)
			}
		}
	}
}

func TestPercentileReferenceManualsMatchToolDescriptions(t *testing.T) {
	checks := map[string]struct {
		body    string
		phrases []string
	}{
		"logjson": {
			body: prompts.LogjsonReference,
			phrases: []string{
				"`$quantile` is the general/default percentile operator",
				"Day-wise: exactly ONE get_logs call over the full half-open start_time_iso/end_time_iso range with one window_aggregate; NEVER one call per day; honor requested timezone.",
				"Never template, merge, or recombine already-aggregated percentile rows",
				"For `attributes[...]` or `resources[...]`",
				"use the canonical anchored numeric `$regex` shown below",
				"Equivalent exotic regex forms are deliberately rejected",
				"excludes non-matching values from percentile calculations; disclose that exclusion in the answer",
				`{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["5","minutes"]`,
				"Discover and use a normalized route field before aggregation",
				"group or aggregate exact raw URI values individually",
				"never normalize or merge URI variants afterward",
				"When `l9_result.partial=true`, preserve returned rows and explicitly disclose partial coverage",
				"never infer or convert units",
				`"function": {"$quantile": [0.99, "attributes['latency_ms']"]}`,
				`"window": ["24", "hours"]`,
			},
		},
		"tracejson": {
			body: prompts.TracejsonReference,
			phrases: []string{
				"`$quantile` is the general/default percentile operator",
				"Top-level `Duration` is already numeric and is measured in nanoseconds",
				"canonical numeric `$regex` `^[0-9]+(?:\\\\.[0-9]+)?$`; it excludes non-matching values from percentile calculations, so disclose that exclusion in the answer",
				"do not add this numeric gate for `Duration`",
				`{"type":"window_aggregate","function":{"$count":[]},"as":"count","window":["5","minutes"]`,
				`"function": {"$quantile": [0.99, "Duration"]}`,
				`"window": ["24", "hours"]`,
			},
		},
		"metrics": {
			body: prompts.MetricsReference,
			phrases: []string{
				"Percentiles are not composable: never average precomputed percentile series",
				"`histogram_quantile(..., rate(...))` or `histogram_quantile(..., increase(...))`",
				"Otherwise use `get_logs` or `get_traces` to aggregate the raw distribution",
			},
		},
	}
	for name, check := range checks {
		for _, phrase := range check.phrases {
			if !strings.Contains(check.body, phrase) {
				t.Errorf("%s reference missing %q", name, phrase)
			}
		}
	}
}

func TestFilterOperatorGuidancePreserved(t *testing.T) {
	for name, body := range map[string]string{
		"get_logs":  prompts.GetLogsDescription,
		"logjson":   prompts.LogjsonReference,
		"tracejson": prompts.TracejsonReference,
	} {
		for _, operator := range []string{"$and", "$or", "$not", "$eq", "$neq", "$regex"} {
			if !strings.Contains(body, operator) {
				t.Errorf("%s filter guidance missing %s", name, operator)
			}
		}
	}
	for name, body := range map[string]string{"get_logs": prompts.GetLogsDescription, "logjson": prompts.LogjsonReference} {
		if !strings.Contains(body, "$containsWords") {
			t.Errorf("%s Body-search guidance missing $containsWords", name)
		}
	}
}

func TestGetServiceLogsDescriptionCriticalRules(t *testing.T) {
	desc := prompts.GetServiceLogsDescription
	if desc == "" {
		t.Fatal("GetServiceLogsDescription is empty — embed directive missing")
	}
	checks := []struct {
		phrase string
		reason string
	}{
		{"get_logs", "must tell model to prefer get_logs for structured attribute queries"},
		{"get_log_attributes", "must tell model to check attributes before body_filters"},
		{"body_filters", "must explicitly label body_filters as last resort"},
		{"last9://reference/service_logs", "must point at the service_logs resource"},
		{"multiple HTTP statuses in one `$or`", "must direct multiple status alternatives to get_logs"},
	}
	for _, c := range checks {
		if !strings.Contains(desc, c.phrase) {
			t.Errorf("GetServiceLogsDescription missing %q: %s", c.phrase, c.reason)
		}
	}
}

// The API rejects expression_args keys other than the bound KPI's own name.
func TestAlertRuleDescriptionsRequireKPINames(t *testing.T) {
	for name, desc := range map[string]string{
		"create_alert":              prompts.CreateAlertDescription,
		"update_alert":              prompts.UpdateAlertDescription,
		"validate_alert_expression": prompts.ValidateAlertExpressionDescription,
	} {
		if !strings.Contains(desc, "exact name") {
			t.Errorf("%s does not require each indicator to use its KPI's exact name", name)
		}
	}
	if strings.Contains(prompts.ValidateAlertExpressionDescription, "x, y, z") {
		t.Error("validate_alert_expression still suggests placeholder indicator names")
	}
	if !strings.Contains(prompts.ValidateAlertExpressionDescription, "Write names with spaces, dots or dashes as-is") {
		t.Error("validate_alert_expression does not say spaced KPI names are written as-is")
	}
}

// The API accepts any work-domain address, not only the org's own domain.
func TestCreateNotificationChannelDescriptionStatesEmailDomains(t *testing.T) {
	desc := prompts.CreateNotificationChannelDescription
	if strings.Contains(desc, "org-domain") || !strings.Contains(desc, "rejects personal-provider addresses") {
		t.Errorf("create_notification_channel misstates which email addresses are accepted: %s", desc)
	}
}

// Delivery joins each attachment to its channel's send_resolved; the attachment row's own
// column is a default nothing reads.
func TestGetNotificationChannelsDescriptionPointsSendResolvedAtChannel(t *testing.T) {
	if !strings.Contains(prompts.GetNotificationChannelsDescription, "resolve notifications follow the channel's own send_resolved") {
		t.Error("get_notification_channels does not say attachment rows' send_resolved is not what delivery uses")
	}
}

// create_alert_from_chart rejects a negative threshold before calling the API.
func TestCreateAlertFromChartDescriptionStatesThresholdFloor(t *testing.T) {
	if !strings.Contains(prompts.CreateAlertFromChartDescription, "must be 0 or more") {
		t.Error("create_alert_from_chart does not say threshold must be 0 or more")
	}
}

// Write tools return their UI link in _meta, which many clients do not show the model.
func TestDescriptionsSayWhereTheLinkIs(t *testing.T) {
	for file, desc := range promptDescriptionFiles {
		for _, promise := range []string{"a link to Alerting", "a link to notification channels"} {
			if strings.Contains(desc, promise) && !strings.Contains(desc, promise+" in the result's _meta.reference_url") {
				t.Errorf("%s promises %q without saying it is in _meta.reference_url", file, promise)
			}
		}
	}
}

func TestCreateAlertDescriptionStatesDuplicateNameRefusal(t *testing.T) {
	if !strings.Contains(prompts.CreateAlertDescription, "already uses (case-insensitive) is refused") {
		t.Error("create_alert does not say a duplicate rule_name is refused")
	}
}

func TestReferenceManualsEmbedded(t *testing.T) {
	for name, body := range map[string]string{
		"logjson":      prompts.LogjsonReference,
		"tracejson":    prompts.TracejsonReference,
		"service_logs": prompts.ServiceLogsReference,
		"metrics":      prompts.MetricsReference,
	} {
		if len(body) < 1000 {
			t.Errorf("%s reference too short (%d chars) — embed may be wrong", name, len(body))
		}
	}
}

func TestAPMServiceDeviationsDescription(t *testing.T) {
	description := strings.ToLower(prompts.GetAPMServiceDeviationsDescription)
	checks := []struct {
		phrase string
		reason string
	}{
		{"equal-duration baseline", "must describe comparative rather than snapshot behavior"},
		{"get_service_summary", "must disambiguate one-window snapshots"},
		{"call this tool first and by itself", "must prevent speculative parallel investigation"},
		{"do not batch speculative corroboration", "must require inspecting the comparison before follow-ups"},
		{"service_name", "must explain fleet and service scope"},
		{"environments remain separate", "must prevent merged environment conclusions"},
		{"server-request workloads", "must state the V1 workload boundary"},
		{"unsupported_workload_shape", "must name the unsupported workload outcome"},
		{"datasource", "must document datasource selection"},
		{"baseline_start_time_iso", "must document explicit baseline windows"},
		{"max_services", "must document fleet result bounds"},
		{"max_operations", "must document operation result bounds"},
		{"regressions", "must describe regression results"},
		{"improvements", "must describe improvement results"},
		{"stable", "must describe stable results"},
		{"evidence quality", "must describe categorical evidence quality"},
		{"operation_apdex_reconciliations", "must explain request-weighted operation evidence"},
		{"unexplained_delta", "must preserve incomplete operation coverage as an explicit residual"},
		{"reported coverage", "must prevent treating partial operation evidence as complete"},
		{"does not establish contribution, attribution, cause, or root cause", "must prevent causal overclaiming"},
	}
	for _, check := range checks {
		if !strings.Contains(description, check.phrase) {
			t.Errorf("description missing %q: %s", check.phrase, check.reason)
		}
	}
}

func TestAPMServiceDeviationsDescriptionDefaultsAndPartialResults(t *testing.T) {
	description := strings.ToLower(prompts.GetAPMServiceDeviationsDescription)
	for _, phrase := range []string{
		"max_services` and `max_operations` each default to 10 and cannot exceed 10",
		"partial_errors",
		"successful evidence remains usable",
		"explicitly qualify conclusions",
		"state the returned evidence quality or limitations",
		"stable`, `no_data`, and `unsupported_workload_shape` as terminal",
		"do not automatically call follow-up tools",
		"all metric queries fail, the tool returns an error",
	} {
		if !strings.Contains(description, phrase) {
			t.Errorf("description missing exact contract wording %q", phrase)
		}
	}
}

func TestWriteToolDescriptionSteerability(t *testing.T) {
	create := prompts.CreateDashboardDescription
	update := prompts.UpdateDashboardDescription
	if create == "" || update == "" {
		t.Fatal("dashboard write descriptions empty — embed missing")
	}
	for _, c := range []struct {
		phrase string
		reason string
	}{
		{"net-new", "must name net-new write intent"},
		{"Create once", "must say create once"},
		{"update_dashboard", "must name the refine tool"},
		{"do not call create_dashboard again", "must forbid same-turn re-create"},
		{"dashboard.id", "must keep the returned id"},
		{"`layout: {x, y, w, h}`", "must document the API panel layout shape"},
		{"`legend: {type, value}`", "must document the API query legend shape"},
		{"Do not use `gridPos` or a panel-level `type`", "must rule out the rejected legacy panel shape"},
	} {
		if !strings.Contains(create, c.phrase) {
			t.Errorf("CreateDashboardDescription missing %q: %s", c.phrase, c.reason)
		}
	}
	for _, c := range []struct {
		phrase string
		reason string
	}{
		{"Prefer this tool", "refine is the default after create"},
		{"after create", "must sequence after create"},
		{"existing dashboard id", "must pass the known id"},
		{"do not call create_dashboard", "must not create-for-refine"},
		{"full replacement", "must state PUT is a full replacement"},
		{"same panel/query shape as create_dashboard", "must retain create's required dashboard payload shape"},
	} {
		if !strings.Contains(update, c.phrase) {
			t.Errorf("UpdateDashboardDescription missing %q: %s", c.phrase, c.reason)
		}
	}
	// Any mention outside the timeout-recovery sentence, not three exact phrasings a reword
	// would slip past.
	if strings.Contains(strings.Replace(create, createRetrySentence("list_dashboards"), "", 1), "list_dashboards") {
		t.Error("CreateDashboardDescription must not require list_dashboards before create")
	}
}

func createRetrySentence(readCall string) string {
	return "On a timeout or connection error the create may or may not have been applied; check by name with " + readCall + " before retrying."
}

// A create whose response was lost may still have landed; a blind retry duplicates it.
func TestCreateToolDescriptionsExplainUnknownOutcome(t *testing.T) {
	for name, c := range map[string]struct{ desc, readCall string }{
		"add_drop_rule":               {prompts.AddDropRuleDescription, "get_drop_rules"},
		"create_alert":                {prompts.CreateAlertDescription, "get_alert_config (rule_name)"},
		"create_alert_from_chart":     {prompts.CreateAlertFromChartDescription, "get_alert_config (rule_name)"},
		"create_dashboard":            {prompts.CreateDashboardDescription, "list_dashboards"},
		"create_notification_channel": {prompts.CreateNotificationChannelDescription, "get_notification_channels (scope=channels)"},
	} {
		if !strings.Contains(c.desc, createRetrySentence(c.readCall)) {
			t.Errorf("%s description does not say how to check a create after a timeout: want %q", name, createRetrySentence(c.readCall))
		}
	}
}

// TestAPMServiceDeviationsDescriptionLookbackFloor pins one sentinel phrase of
// the short-lookback guidance on the markdown contract surface so a future
// edit cannot silently strip the bullet. The behavior itself (integer lookback
// below 2 collapsing to zero completed buckets) is guarded by the resolver
// boundary test in internal/apm; this only checks the guidance is present.
func TestAPMServiceDeviationsDescriptionLookbackFloor(t *testing.T) {
	description := strings.ToLower(prompts.GetAPMServiceDeviationsDescription)
	if !strings.Contains(description, "short lookbacks are unreliable") {
		t.Error("description missing short-lookback floor guidance")
	}
}

// The routing section must quote the window parameter's default and cap, or
// agents pass a full hour when the user asks for current alerts.
func TestGetAlertsRoutingStatesDefaultWindow(t *testing.T) {
	_, routing, _ := strings.Cut(prompts.GetAlertsDescription, "## When to use")
	routing, _, _ = strings.Cut(routing, "##")
	for _, phrase := range []string{"default 15 minutes", "at most 1 hour"} {
		if !strings.Contains(routing, phrase) {
			t.Errorf("get_alerts When to use section missing %q:\n%s", phrase, routing)
		}
	}
}
