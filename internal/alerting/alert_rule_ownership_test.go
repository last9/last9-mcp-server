package alerting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ownerServer answers the rule lookup with lookupStatus/lookupBody and records every
// request after it; any write it sees is answered 2xx so only the guard can fail.
type ownerServer struct {
	*httptest.Server
	writes []string
}

func newOwnerServer(t *testing.T, lookupStatus int, lookupBody string) *ownerServer {
	s := &ownerServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.Method == http.MethodGet {
			w.WriteHeader(lookupStatus)
			_, _ = w.Write([]byte(lookupBody))
			return
		}
		s.writes = append(s.writes, r.Method+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{"id":"rule-2"}`))
	}))
	t.Cleanup(s.Close)
	return s
}

func ruleWriteCalls(s *ownerServer) map[string]func() (*mcp.CallToolResult, error) {
	cfg := testAlertMutationConfig(s.URL)
	ctx := context.Background()
	disabled := true
	return map[string]func() (*mcp.CallToolResult, error){
		"patch_alert": func() (*mcp.CallToolResult, error) {
			r, _, err := NewPatchAlertHandler(s.Client(), cfg)(ctx, nil, PatchAlertArgs{EntityID: "entity-1", ID: "rule-1", IsDisabled: &disabled})
			return r, err
		},
		"update_alert": func() (*mcp.CallToolResult, error) {
			r, _, err := NewUpdateAlertHandler(s.Client(), cfg)(ctx, nil, UpdateAlertArgs{EntityID: "entity-1", ID: "rule-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(nil)}})
			return r, err
		},
		"delete_alert": func() (*mcp.CallToolResult, error) {
			r, _, err := NewDeleteAlertHandler(s.Client(), cfg)(ctx, nil, DeleteAlertArgs{EntityID: "entity-1", ID: "rule-1"})
			return r, err
		},
	}
}

func TestRuleWritesRefuseRuleOwnedByAnotherGroup(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		status           int
	}{
		{"other group", `{"id":"rule-1","entity_id":"entity-2"}`, "belongs to alert group entity-2, not entity-1", http.StatusOK},
		{"owner unknown", `{"id":"rule-1"}`, "could not confirm", http.StatusOK},
		{"rule not found", `{"error":"Rule not found"}`, "no alert rule rule-1", http.StatusNotFound},
	} {
		s := newOwnerServer(t, tt.status, tt.body)
		for tool, call := range ruleWriteCalls(s) {
			t.Run(tt.name+"/"+tool, func(t *testing.T) {
				s.writes = nil
				result, err := call()
				if err != nil || result == nil || !result.IsError || !strings.Contains(resultText(result), tt.want) {
					t.Fatalf("result=%s err=%v", resultText(result), err)
				}
				if !strings.Contains(resultText(result), "nothing was changed") {
					t.Fatalf("result does not say nothing changed: %s", resultText(result))
				}
				if len(s.writes) != 0 {
					t.Fatalf("write sent despite failed ownership check: %v", s.writes)
				}
			})
		}
	}
}

func TestRuleWritesProceedForOwnedRule(t *testing.T) {
	// Entity ids are UUIDs; the API may echo them in a different case than the caller sent.
	s := newOwnerServer(t, http.StatusOK, `{"id":"rule-1","entity_id":"ENTITY-1"}`)
	for tool, call := range ruleWriteCalls(s) {
		t.Run(tool, func(t *testing.T) {
			s.writes = nil
			result, err := call()
			if err != nil || result == nil || result.IsError {
				t.Fatalf("result=%s err=%v", resultText(result), err)
			}
			if len(s.writes) != 1 {
				t.Fatalf("writes = %v", s.writes)
			}
		})
	}
}

func TestRuleWritesRelayLookupFailure(t *testing.T) {
	s := newOwnerServer(t, http.StatusBadRequest, `{"error":"Invalid rule ID"}`)
	for tool, call := range ruleWriteCalls(s) {
		t.Run(tool, func(t *testing.T) {
			s.writes = nil
			result, err := call()
			if err == nil || !strings.Contains(err.Error(), "Invalid rule ID") {
				t.Fatalf("result=%s err=%v", resultText(result), err)
			}
			if len(s.writes) != 0 {
				t.Fatalf("writes = %v", s.writes)
			}
		})
	}
}
