package prompts_test

import (
	"strings"
	"testing"

	"last9-mcp/internal/prompts"
)

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
		{"Ops: `$and`/`$or`/`$not`", "must retain a useful filter operator list"},
		{"NOT SQL", "must clarify logjson_query is not SQL"},
		{"$neq", "must document existence idiom"},
		{"start_time_iso", "must document absolute time on tool args"},
		{"resources['last9.tenant']", "must document tenant scoping"},
		{"resources['deployment.environment']", "must document env scoping"},
		{"last9://reference/logjson", "must point at the logjson resource"},
		{"community_member_id", "must document bare single-token field mistake"},
		{"attributes['key']", "must require attributes/resources wrapping"},
		{"`$quantile` is the general/default percentile operator", "must default to approximate percentile aggregation"},
		{"\"function\":{\"$quantile\":[0.99,\"attributes['latency_ms']\"]},\"as\":\"p99\",\"window\":[\"24\",\"hours\"]", "must show a complete day-wise percentile window"},
		{"Day-wise: exactly ONE get_logs call over the full half-open start_time_iso/end_time_iso range with one window_aggregate; NEVER one call per day; honor requested timezone.", "must require one timezone-aware full-range day-wise call"},
		{"Never template/merge/recombine aggregated percentile rows", "must forbid client-side percentile reconstruction"},
		{"use the canonical anchored numeric `$regex` shown", "must require the deliberate canonical numeric allowlist"},
		{"excludes non-matching values from percentile calculations; disclose that exclusion in the answer", "must disclose the numeric filter's effect"},
		{"Use a discovered normalized route", "must discover normalized route semantics before aggregation"},
		{"Raw URI: aggregate exact values only; never normalize/merge variants afterward", "must allow exact raw URI aggregation without unsafe normalization"},
		{"If `l9_result.partial=true`, preserve rows and disclose partial coverage", "must tie partial disclosure to result metadata"},
		{"never infer/convert", "must prevent inferred latency units"},
		{"ALL → `$and` of one `$containsWords` per word", "must document all-word Body searches"},
		{"ANY → `$or`", "must document any-word Body searches"},
		{"never `$icontainsWords`", "must avoid the slower case-insensitive word operator"},
	}
	for _, c := range checks {
		if !strings.Contains(desc, c.phrase) {
			t.Errorf("GetLogsDescription missing %q: %s", c.phrase, c.reason)
		}
	}
	if strings.Contains(desc, "window_minutes") {
		t.Error("GetLogsDescription must NOT contain deprecated 'window_minutes' key")
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
		{"$neq", "must document existence idiom"},
		{"aggregates", "must document aggregate key name"},
		{"groupby", "must document groupby key name"},
		{"resources['last9.tenant']", "must document tenant scoping"},
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
	}
	for _, c := range checks {
		if !strings.Contains(desc, c.phrase) {
			t.Errorf("GetServiceLogsDescription missing %q: %s", c.phrase, c.reason)
		}
	}
}

func TestWhaleDescriptionsBounded(t *testing.T) {
	budgets := map[string]int{
		"get_logs_base":         2900,
		"get_traces_base":       2200,
		"get_service_logs_base": 2000,
	}
	bodies := map[string]string{
		"get_logs_base":         prompts.GetLogsDescription,
		"get_traces_base":       prompts.GetTracesDescription,
		"get_service_logs_base": prompts.GetServiceLogsDescription,
	}
	for name, body := range bodies {
		if len(body) == 0 {
			t.Errorf("%s empty", name)
		}
		budget := budgets[name]
		if len(body) > budget {
			t.Errorf("%s length %d exceeds %d-char budget", name, len(body), budget)
		}
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
