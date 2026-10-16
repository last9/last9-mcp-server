package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"last9-mcp/internal/utils"
)

func TestAlertIntelGuidanceFirstLine(t *testing.T) {
	for _, tt := range []struct {
		status int
		body   string
		first  string
		extra  string
	}{
		{400, `unknown chart key: p95`, "alert intelligence API returned status 400 (coverage_miss): unknown chart key: p95", "not covered by the alert-coverage catalog"},
		{403, "Not allowed for viewer role", "alert intelligence API returned status 403 (permissions): Not allowed for viewer role", "viewer-role tokens are rejected"},
		{409, "duplicate rule name", "alert intelligence API returned status 409 (duplicate_name): duplicate rule name", "already exists"},
		{502, "", "alert intelligence API returned status 502 (upstream): Bad Gateway", "retry once"},
	} {
		got := alertIntelGuidance(&utils.UpstreamHTTPError{StatusCode: tt.status, Body: tt.body})
		if !strings.HasPrefix(got, tt.first+"\n") || !strings.Contains(got, tt.extra) {
			t.Errorf("status %d: %s", tt.status, got)
		}
	}
}

func conflictServer(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveRuleOwner(w, r) {
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("rule is locked by another editor"))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestAlertWriteToolsRelayUpstreamBody(t *testing.T) {
	s := conflictServer(t)
	cfg := testAlertMutationConfig(s.URL)
	ctx := context.Background()
	name := "latency"
	enabled := true
	_, _, createErr := NewCreateAlertHandler(s.Client(), cfg)(ctx, nil, CreateAlertArgs{EntityID: "e", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
	_, _, updateErr := NewUpdateAlertHandler(s.Client(), cfg)(ctx, nil, UpdateAlertArgs{EntityID: "e", ID: "r", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
	_, _, patchErr := NewPatchAlertHandler(s.Client(), cfg)(ctx, nil, PatchAlertArgs{EntityID: "e", ID: "r", IsDisabled: &enabled})
	_, _, deleteErr := NewDeleteAlertHandler(s.Client(), cfg)(ctx, nil, DeleteAlertArgs{EntityID: "e", ID: "r"})
	_, _, muteErr := NewMuteAlertGroupHandler(s.Client(), cfg)(ctx, nil, MuteAlertGroupArgs{EntityID: "e", Until: 1})
	for tool, err := range map[string]error{"create": createErr, "update": updateErr, "patch": patchErr, "delete": deleteErr, "mute": muteErr} {
		if err == nil || !strings.Contains(err.Error(), "HTTP 409") || !strings.Contains(err.Error(), "rule is locked by another editor") {
			t.Errorf("%s: %v", tool, err)
		}
	}
}

func TestWithUpstreamBodyJoinsSentencesOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.WriteHeader(http.StatusConflict)
	_, _ = rec.WriteString("rule is locked")
	err := withUpstreamBody(utils.NewUpstreamHTTPError(rec.Result(), "patch alert rule"))
	if strings.Contains(err.Error(), "..") || !strings.HasSuffix(err.Error(), ". Upstream response: rule is locked") {
		t.Fatalf("err = %q", err)
	}
}

func TestDeleteAlertForbiddenNamesDeleteScope(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveRuleOwner(w, r) {
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"You don't have access to this resource."}`))
	}))
	defer s.Close()
	result, _, err := NewDeleteAlertHandler(s.Client(), testAlertMutationConfig(s.URL))(context.Background(), nil, DeleteAlertArgs{EntityID: "entity-1", ID: "rule-1"})
	if err != nil || result == nil || !result.IsError || !strings.Contains(resultText(result), "delete-scope token") {
		t.Fatalf("result=%s err=%v", resultText(result), err)
	}
}

func TestReassignAlertRelaysUpstreamBodyOn409(t *testing.T) {
	s := newReassignServer(t, http.StatusConflict, "claim changed concurrently")
	_, _, err := runReassign(s, ReassignAlertArgs{OwnerUserID: "u2"})
	if err == nil || !strings.Contains(err.Error(), "claim changed concurrently") {
		t.Fatalf("err = %v", err)
	}
}
