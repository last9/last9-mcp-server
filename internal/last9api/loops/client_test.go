package loops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/last9api"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"
)

func testClient(t *testing.T, handler http.HandlerFunc) *last9api.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return last9api.NewClient(srv.Client(), models.Config{
		APIBaseURL: srv.URL,
		TokenManager: &auth.TokenManager{
			AccessToken: "tok",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	})
}

func TestProbeEnabled_NotEnabled(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loops" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error_details":{"code":"LOOPS_NOT_ENABLED"}}`)
	})
	enabled, err := ProbeEnabled(context.Background(), c)
	if enabled || !IsNotEnabled(err) {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}
	st := StatusResponse(false, err)
	if st.Enabled || st.Reason != "LOOPS_NOT_ENABLED" || st.Message == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestMapError_RequiresExpectedHTTPStatusAndCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "untyped prose", err: errors.New("LOOPS_NOT_ENABLED")},
		{name: "unauthorized", err: &utils.UpstreamHTTPError{StatusCode: http.StatusUnauthorized, Body: `{"code":"LOOPS_NOT_ENABLED"}`}},
		{name: "different code", err: &utils.UpstreamHTTPError{StatusCode: http.StatusUnprocessableEntity, Body: `{"code":"OTHER"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsNotEnabled(mapError(tt.err)) {
				t.Fatalf("mapped %v as disabled", tt.err)
			}
		})
	}
}

func TestProbeEnabled_RequiresResponseBody(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	enabled, err := ProbeEnabled(context.Background(), c)
	if enabled || err == nil {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}
}

func TestList_RejectsOversizedResponse(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"`+strings.Repeat("x", 1<<20)+`"}]`)
	})
	items, err := List(context.Background(), c, "")
	if items != nil || err == nil {
		t.Fatalf("item count=%d err=%v", len(items), err)
	}
}

func TestList_FiltersAndDecodes(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"loops":[
			{"id":"aut1","name":"Prod RCA","status":"draft","enabled":false,"actionMode":"simulated"},
			{"id":"aut2","name":"Other","status":"published","enabled":true,"action_mode":"live"}
		]}`)
	})
	items, err := List(context.Background(), c, "rca")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "aut1" || items[0].ActionMode != "simulated" || items[0].Enabled {
		t.Fatalf("items = %+v", items)
	}
}

func TestGet_RedactsSecretsAndReportsGaps(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loops/aut1" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{
			"id":"aut1","name":"RCA","status":"draft","enabled":false,"action_mode":"simulated",
			"graph":{"nodes":[{"type":"slack","channel":"","token":"sekrit"}]}
		}`)
	})
	detail, err := Get(context.Background(), c, "aut1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != "aut1" || len(detail.SetupGaps) == 0 {
		t.Fatalf("detail = %+v", detail)
	}
	var graph map[string]any
	if err := json.Unmarshal(detail.Graph, &graph); err != nil {
		t.Fatal(err)
	}
	nodes := graph["nodes"].([]any)
	node := nodes[0].(map[string]any)
	if node["token"] != "[redacted]" {
		t.Fatalf("token not redacted: %#v", node)
	}
	if !strings.Contains(strings.Join(detail.SetupGaps, " "), "slack channel") {
		t.Fatalf("setup gaps = %v", detail.SetupGaps)
	}
}

func TestListTemplates_CapabilityFilter(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loops/templates" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"templates":[
			{"slug":"investigate-last9-alert","name":"Investigate","requiredParams":["slack_channel"],"capability":"investigation"},
			{"slug":"daily-last9-dashboard-report","name":"Report","capability":"report"}
		]}`)
	})
	items, err := ListTemplates(context.Background(), c, "investigation")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Slug != "investigate-last9-alert" || len(items[0].RequiredParams) != 1 {
		t.Fatalf("items = %+v", items)
	}
}
