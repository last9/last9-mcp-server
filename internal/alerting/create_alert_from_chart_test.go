package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"last9-mcp/internal/deeplink"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

func createAlertFromChartTestConfig(baseURL string) models.Config {
	cfg := testAlertMutationConfig(baseURL)
	cfg.OrgSlug = "acme"
	cfg.ClusterID = "cluster-1"
	return cfg
}

func TestCreateAlertFromChartAllDefaultsOmitted(t *testing.T) { // AE1
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotBody, _ = io.ReadAll(r.Body)
		}
		_, _ = w.Write([]byte(`{"id":"rule-9","group_id":"grp-2","kpi_id":"kpi-3"}`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(result))
	}

	var raw map[string]any
	if err := json.Unmarshal(gotBody, &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"threshold", "threshold_operator", "eval_window", "bad_minutes", "severity"} {
		if _, ok := raw[k]; ok {
			t.Errorf("omitted optional %q leaked into request: %s", k, gotBody)
		}
	}

	text := resultText(result)
	for _, want := range []string{
		"rule-9", "grp-2", "kpi-3", "error_ratio",
		"threshold_operator: > (backend default)",
		"threshold: 0.01 (backend default)",
		"eval_window: 5 minutes (backend default)",
		"bad_minutes: 3 (backend default)",
		"severity: breach (backend default for static chart rules)",
		draftSentence,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("result missing %q:\n%s", want, text)
		}
	}

	wantMeta := deeplink.ToMeta(deeplink.NewBuilder("acme", "cluster-1").BuildAlertingGroupsLink())
	if !reflect.DeepEqual(result.Meta, wantMeta) {
		t.Errorf("meta = %+v, want %+v", result.Meta, wantMeta)
	}
}

func TestCreateAlertFromChartExplicitScoreUnitThreshold(t *testing.T) { // AE4
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			gotBody, _ = io.ReadAll(r.Body)
		}
		_, _ = w.Write([]byte(`{"id":"rule-apdex","group_id":"grp-apdex"}`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "apdex",
		ServiceName: "checkout",
		SignalKey:   "apdex_score",
		Name:        "checkout-apdex",
		Threshold:   float64Ptr(0.9),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", resultText(result))
	}
	if !bytes.Contains(gotBody, []byte(`"threshold":"0.9"`)) {
		t.Errorf("explicit threshold not passed through as string: %s", gotBody)
	}
	if bytes.Contains(gotBody, []byte(`"threshold":0.9`)) {
		t.Errorf("threshold serialized as number, want string: %s", gotBody)
	}
	text := resultText(result)
	if strings.Contains(text, "0.01") {
		t.Errorf("default threshold 0.01 mentioned despite explicit value:\n%s", text)
	}
	if !strings.Contains(text, "threshold: 0.9") || strings.Contains(text, "0.9 (backend default)") {
		t.Errorf("explicit threshold not reported as applied:\n%s", text)
	}
}

func float64Ptr(v float64) *float64 { return &v }

func TestCreateAlertFromChartLocalValidationRejects(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	valid := CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "apdex",
		ServiceName: "checkout",
		SignalKey:   "apdex_score",
		Name:        "checkout-apdex",
	}
	tests := []struct {
		name   string
		mutate func(*CreateAlertFromChartArgs)
	}{
		{"empty name", func(a *CreateAlertFromChartArgs) { a.Name = "   " }},
		{"negative threshold", func(a *CreateAlertFromChartArgs) { a.Threshold = float64Ptr(-0.5) }},
		{"bad_minutes exceeds eval_window", func(a *CreateAlertFromChartArgs) {
			w, b := 5, 6
			a.EvalWindow, a.BadMinutes = &w, &b
		}},
		{"bad_minutes exceeds default window", func(a *CreateAlertFromChartArgs) {
			b := 6
			a.BadMinutes = &b // default eval window is 5
		}},
		{"eval_window over 60", func(a *CreateAlertFromChartArgs) {
			w := 61
			a.EvalWindow = &w
		}},
		{"unknown operator", func(a *CreateAlertFromChartArgs) { a.ThresholdOperator = "~" }},
		{"unknown severity", func(a *CreateAlertFromChartArgs) { a.Severity = "critical" }},
		{"uncovered surface", func(a *CreateAlertFromChartArgs) { a.Surface = "dashboards" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := valid
			tt.mutate(&args)
			handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
			result, _, err := handler(context.Background(), nil, args)
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("expected tool error for %s, got: %s", tt.name, resultText(result))
			}
		})
	}
	if requests != 0 {
		t.Fatalf("server received %d requests for locally invalid args", requests)
	}
}

func TestCreateAlertFromChartDuplicateNameGuidance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "duplicate rule name", http.StatusConflict)
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got: %s", resultText(result))
	}
	text := resultText(result)
	for _, want := range []string{"get_alert_config", "entity_id", "get_entity_alert_rules", "never retry blindly", "timeout", "duplicate"} {
		if !strings.Contains(text, want) {
			t.Errorf("duplicate-name guidance missing %q:\n%s", want, text)
		}
	}
}

func TestAlertIntelEntityAttrsCannotOverrideReservedKeys(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	if _, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
		Env:         "prod",
		Attributes:  map[string]string{"serviceName": "evil", "env": "staging", "region": "us-east-1"},
	}); err != nil {
		t.Fatal(err)
	}
	var req struct {
		Entity map[string]string `json:"entity"`
	}
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatal(err)
	}
	if req.Entity["serviceName"] != "checkout" {
		t.Errorf("serviceName overridden via attributes: %v", req.Entity)
	}
	if req.Entity["env"] != "prod" {
		t.Errorf("env overridden via attributes: %v", req.Entity)
	}
	if req.Entity["region"] != "us-east-1" {
		t.Errorf("non-reserved attribute dropped: %v", req.Entity)
	}
}

func TestCreateAlertFromChartRefusedConnectionIsNotUnknownOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	client := server.Client()
	server.Close() // nothing is listening, so the request never leaves this process

	handler := NewCreateAlertFromChartHandler(client, createAlertFromChartTestConfig(baseURL))
	_, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	var unknown *utils.WriteOutcomeUnknownError
	if err == nil || errors.As(err, &unknown) {
		t.Fatalf("a refused connection was not applied; want a plain error, got %v", err)
	}
}

// A 2xx whose body cannot be read most likely created the rule, so it must not read as a
// plain failure the agent would retry.
func TestCreateAlertFromChartGarbageBodyIsUnknownOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json{{`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	var unknown *utils.WriteOutcomeUnknownError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected an unknown-outcome error, got result %v err %v", result, err)
	}
}

func TestCreateAlertFromChartSuccessWithoutRuleID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err != nil {
		t.Fatalf("no-rule-id must surface as tool error result, got Go error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result, got: %s", resultText(result))
	}
	text := resultText(result)
	for _, want := range []string{"no rule id", "get_alert_config"} {
		if !strings.Contains(text, want) {
			t.Errorf("no-rule-id guidance missing %q:\n%s", want, text)
		}
	}
}

func TestCreateAlertFromChartPermissionsGuidance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "viewer role not allowed", http.StatusForbidden)
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface:     "discover-service",
		ChartKey:    "error_rate",
		ServiceName: "checkout",
		SignalKey:   "error_ratio",
		Name:        "checkout-error-rate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("expected tool error, got: %s", resultText(result))
	}
	text := resultText(result)
	if !strings.Contains(text, "credentials") || !strings.Contains(text, "role") {
		t.Errorf("permission guidance missing credential framing:\n%s", text)
	}
	if strings.Contains(text, "Discover page") {
		t.Errorf("permission guidance must be distinct from coverage text:\n%s", text)
	}
}

func TestCreateAlertFromChartPassesGroupIDAndDescription(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"rule-1","group_id":"grp-1"}`))
	}))
	defer server.Close()

	handler := NewCreateAlertFromChartHandler(server.Client(), createAlertFromChartTestConfig(server.URL))
	result, _, err := handler(context.Background(), nil, CreateAlertFromChartArgs{
		Surface: "discover-service", ChartKey: "error_rate", ServiceName: "checkout", SignalKey: "error_rate", Name: "checkout-errors",
		GroupID: " grp-1 ", Description: "Checkout errors above baseline",
	})
	if err != nil || result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if got["group_id"] != "grp-1" || got["description"] != "Checkout errors above baseline" {
		t.Errorf("body = %v", got)
	}
	if _, ok := got["algorithm"]; ok {
		t.Errorf("algorithm must not be sent: %v", got)
	}
}

const draftSentence = "Created as a disabled draft; it will not fire. Promote it with patch_alert is_disabled=false only after the user confirms."

type chartRequest struct{ method, path, body string }

// chartDraftServer answers the chart create with createReply and the follow-up disable
// with patch, recording every request in order.
func chartDraftServer(t *testing.T, createReply string, patch http.HandlerFunc) (*httptest.Server, *[]chartRequest) {
	t.Helper()
	var reqs []chartRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		reqs = append(reqs, chartRequest{r.Method, r.URL.Path, string(raw)})
		if r.Method == http.MethodPatch {
			patch(w, r)
			return
		}
		_, _ = w.Write([]byte(createReply))
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func patchOK(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }

var checkoutChartArgs = CreateAlertFromChartArgs{
	Surface: "discover-service", ChartKey: "error_rate", ServiceName: "checkout", SignalKey: "error_ratio", Name: "checkout-error-rate",
}

func TestCreateAlertFromChartLeavesRuleDisabled(t *testing.T) {
	srv, reqs := chartDraftServer(t, `{"id":"rule-9","group_id":"grp-2"}`, patchOK)
	result, _, err := NewCreateAlertFromChartHandler(srv.Client(), createAlertFromChartTestConfig(srv.URL))(context.Background(), nil, checkoutChartArgs)
	if err != nil || result.IsError {
		t.Fatalf("result=%s err=%v", resultText(result), err)
	}
	if len(*reqs) != 2 {
		t.Fatalf("requests = %+v, want create then disable", *reqs)
	}
	if got := (*reqs)[0]; got.method != http.MethodPost || got.path != "/alert_intelligence" {
		t.Errorf("first request = %s %s, want the chart create", got.method, got.path)
	}
	want := chartRequest{http.MethodPatch, "/entities/grp-2/alert-rules/rule-9", `{"is_disabled":true}`}
	if got := (*reqs)[1]; got != want {
		t.Errorf("second request = %+v, want %+v", got, want)
	}
	text := resultText(result)
	if !strings.Contains(text, draftSentence) {
		t.Errorf("result lacks the create_alert draft sentence:\n%s", text)
	}
	if strings.Contains(text, "active now") || strings.Contains(text, "fire immediately") {
		t.Errorf("result still says the rule is live:\n%s", text)
	}
}

func TestCreateAlertFromChartDisablesInPassedGroupWhenResponseOmitsIt(t *testing.T) {
	srv, reqs := chartDraftServer(t, `{"id":"rule-9"}`, patchOK)
	args := checkoutChartArgs
	args.GroupID = " grp-7 "
	result, _, err := NewCreateAlertFromChartHandler(srv.Client(), createAlertFromChartTestConfig(srv.URL))(context.Background(), nil, args)
	if err != nil || result.IsError {
		t.Fatalf("result=%s err=%v", resultText(result), err)
	}
	if len(*reqs) != 2 || (*reqs)[1].path != "/entities/grp-7/alert-rules/rule-9" {
		t.Fatalf("requests = %+v, want the disable in grp-7", *reqs)
	}
	if text := resultText(result); !strings.Contains(text, "Created alert rule rule-9 in group grp-7 ") {
		t.Errorf("result does not name the group the rule was disabled in:\n%s", text)
	}
}

// Once the create has landed, a failed disable must say the rule exists and may be live,
// never read as a failed create the agent would retry into a duplicate.
func TestCreateAlertFromChartReportsFailedDisable(t *testing.T) {
	for _, tt := range []struct {
		name        string
		createReply string
		patch       http.HandlerFunc
		patches     int
		want        []string
	}{
		{"disable rejected", `{"id":"rule-9","group_id":"grp-2"}`,
			func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) }, 1,
			[]string{"rule-9", "may be live", "patch_alert entity_id=grp-2 id=rule-9 is_disabled=true", "delete_alert", "do not retry create_alert_from_chart"}},
		{"disable outcome unknown", `{"id":"rule-9","group_id":"grp-2"}`,
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }, 1,
			[]string{"rule-9", "may be live", "patch_alert entity_id=grp-2 id=rule-9 is_disabled=true", "do not retry create_alert_from_chart"}},
		{"no alert group to disable in", `{"id":"rule-9"}`, patchOK, 0,
			[]string{"rule-9", "may be live", "get_alert_config", "is_disabled=true", "do not retry create_alert_from_chart"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv, reqs := chartDraftServer(t, tt.createReply, tt.patch)
			result, _, err := NewCreateAlertFromChartHandler(srv.Client(), createAlertFromChartTestConfig(srv.URL))(context.Background(), nil, checkoutChartArgs)
			if err != nil {
				t.Fatalf("a landed create must be a tool result, got Go error %v", err)
			}
			if !result.IsError {
				t.Fatalf("want isError, got: %s", resultText(result))
			}
			patches := 0
			for _, r := range *reqs {
				if r.method == http.MethodPatch {
					patches++
				}
			}
			if patches != tt.patches {
				t.Errorf("disable attempts = %d, want %d (never retried): %+v", patches, tt.patches, *reqs)
			}
			text := resultText(result)
			for _, want := range tt.want {
				if !strings.Contains(text, want) {
					t.Errorf("result missing %q:\n%s", want, text)
				}
			}
			if strings.Contains(text, draftSentence) {
				t.Errorf("failed disable must not claim a draft:\n%s", text)
			}
		})
	}
}
