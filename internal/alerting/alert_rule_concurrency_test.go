package alerting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type storedRule struct {
	Name       string
	Properties json.RawMessage
}

// ruleStore fakes one alert group's rules: reads serve the store, writes replace it, and
// every read waits at the barrier so two unserialized calls both read before either writes.
type ruleStore struct {
	mu      sync.Mutex
	entity  string
	rules   map[string]storedRule
	posts   int
	nextID  int
	barrier *utils.ReadBarrier
	// replaced, when set, is closed by a PUT; a PATCH waits for it (bounded) so an
	// unserialized patch always lands after a concurrent update replaced the rule.
	replaced chan struct{}
}

func newRuleStore(t *testing.T, entity string, rules map[string]storedRule) (*ruleStore, *httptest.Server) {
	t.Helper()
	store := &ruleStore{entity: entity, rules: rules, barrier: utils.NewReadBarrier(2, 300*time.Millisecond)}
	server := httptest.NewServer(http.HandlerFunc(store.serve))
	t.Cleanup(server.Close)
	return store, server
}

func (s *ruleStore) serve(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "entities" || parts[1] != s.entity || parts[2] != "alert-rules" {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		s.barrier.Wait()
	}
	if r.Method == http.MethodPatch && s.replaced != nil {
		select {
		case <-s.replaced:
		case <-time.After(300 * time.Millisecond):
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case len(parts) == 3 && r.Method == http.MethodGet:
		var rules []map[string]string
		for id, rule := range s.rules {
			rules = append(rules, map[string]string{"id": id, "entity_id": s.entity, "rule_name": rule.Name})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"rules": rules})
	case len(parts) == 3 && r.Method == http.MethodPost:
		var body struct {
			RuleName string `json:"rule_name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.posts++
		id := s.newID()
		s.rules[id] = storedRule{Name: body.RuleName}
		_, _ = fmt.Fprintf(w, `{"id":%q}`, id)
	case len(parts) == 4:
		s.serveRule(w, r, parts[3])
	default:
		http.NotFound(w, r)
	}
}

func (s *ruleStore) serveRule(w http.ResponseWriter, r *http.Request, id string) {
	rule, ok := s.rules[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "entity_id": s.entity, "rule_name": rule.Name, "properties": rule.Properties})
	case http.MethodPatch:
		var body struct {
			Properties json.RawMessage `json:"properties"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Properties != nil {
			rule.Properties = body.Properties
			s.rules[id] = rule
		}
	case http.MethodPut:
		// The API may replace the rule id on update.
		delete(s.rules, id)
		if s.replaced != nil {
			close(s.replaced)
		}
		replacement := s.newID()
		s.rules[replacement] = rule
		_, _ = fmt.Fprintf(w, `{"id":%q}`, replacement)
	default:
		http.NotFound(w, r)
	}
}

func (s *ruleStore) newID() string {
	s.nextID++
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", s.nextID)
}

func (s *ruleStore) properties(t *testing.T, id string) AlertProperties {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var props AlertProperties
	if err := json.Unmarshal(s.rules[id].Properties, &props); err != nil {
		t.Fatalf("stored properties %s: %v", s.rules[id].Properties, err)
	}
	return props
}

type toolCall func() (*mcp.CallToolResult, error)

func runConcurrently(calls ...toolCall) ([]*mcp.CallToolResult, []error) {
	results := make([]*mcp.CallToolResult, len(calls))
	errs := make([]error, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Go(func() { results[i], errs[i] = call() })
	}
	wg.Wait()
	return results, errs
}

const concurrencyRuleID = "11111111-1111-1111-1111-111111111111"

func TestConcurrentPatchAlertKeepsBothPropertyChanges(t *testing.T) {
	store, server := newRuleStore(t, "entity-1", map[string]storedRule{
		concurrencyRuleID: {Name: "latency", Properties: json.RawMessage(`{"description":"old description"}`)},
	})
	handler := NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))
	patch := func(props AlertProperties) toolCall {
		return func() (*mcp.CallToolResult, error) {
			result, _, err := handler(context.Background(), nil, PatchAlertArgs{EntityID: "entity-1", ID: concurrencyRuleID, Properties: &props})
			return result, err
		}
	}
	results, errs := runConcurrently(
		patch(AlertProperties{Runbook: &AlertRunbook{Link: "https://runbooks.example/latency"}}),
		patch(AlertProperties{Description: "new description"}),
	)
	for i := range results {
		if errs[i] != nil || results[i].IsError {
			t.Fatalf("call %d: result=%s err=%v", i, resultText(results[i]), errs[i])
		}
	}
	got := store.properties(t, concurrencyRuleID)
	if got.Description != "new description" || got.Runbook == nil || got.Runbook.Link != "https://runbooks.example/latency" {
		t.Fatalf("stored properties = %+v, want both changes", got)
	}
}

func TestConcurrentUpdateAndPatchAlertNeverPatchReplacedRule(t *testing.T) {
	store, server := newRuleStore(t, "entity-1", map[string]storedRule{
		concurrencyRuleID: {Name: "latency", Properties: json.RawMessage(`{}`)},
	})
	store.replaced = make(chan struct{})
	cfg := testAlertMutationConfig(server.URL)
	name := "latency"
	disabled := true
	results, errs := runConcurrently(
		func() (*mcp.CallToolResult, error) {
			result, _, err := NewUpdateAlertHandler(server.Client(), cfg)(context.Background(), nil, UpdateAlertArgs{
				EntityID: "entity-1", ID: concurrencyRuleID, AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
			return result, err
		},
		func() (*mcp.CallToolResult, error) {
			result, _, err := NewPatchAlertHandler(server.Client(), cfg)(context.Background(), nil, PatchAlertArgs{
				EntityID: "entity-1", ID: concurrencyRuleID, IsDisabled: &disabled})
			return result, err
		},
	)
	if errs[0] != nil || results[0].IsError {
		t.Fatalf("update_alert: result=%s err=%v", resultText(results[0]), errs[0])
	}
	if errs[1] != nil {
		t.Fatalf("patch_alert reached the replaced rule: %v", errs[1])
	}
	if results[1].IsError && !strings.Contains(resultText(results[1]), "replaced by update_alert") {
		t.Fatalf("patch_alert: %s", resultText(results[1]))
	}
}

func TestConcurrentCreateAlertWithSameNameCreatesOnce(t *testing.T) {
	// Each pair is equal under the duplicate check's strings.EqualFold.
	for _, names := range [][2]string{
		{"High latency", " high latency "},
		{"Σ latency", "ς latency"},
		{"K latency", "k latency"}, // Kelvin sign
	} {
		t.Run(names[0]+"/"+names[1], func(t *testing.T) {
			store, server := newRuleStore(t, "entity-1", map[string]storedRule{})
			handler := NewCreateAlertHandler(server.Client(), testAlertMutationConfig(server.URL))
			create := func(name string) toolCall {
				return func() (*mcp.CallToolResult, error) {
					result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, CreateAlertArgs{
						EntityID: "entity-1", AlertRuleMutation: AlertRuleMutation{AlertRule: validAlertRulePayload(&name)}})
					return result, err
				}
			}
			results, errs := runConcurrently(create(names[0]), create(names[1]))
			for i := range results {
				if errs[i] != nil {
					t.Fatalf("call %d: %v", i, errs[i])
				}
			}
			store.mu.Lock()
			posts := store.posts
			store.mu.Unlock()
			if posts != 1 {
				t.Fatalf("POST count = %d, want 1", posts)
			}
			if results[0].IsError == results[1].IsError {
				t.Fatalf("want one create and one duplicate refusal: %q / %q", resultText(results[0]), resultText(results[1]))
			}
		})
	}
}

func TestAlertWritesValidateBeforeWaitingOnRuleLock(t *testing.T) {
	unlock, err := lockAlertRule(context.Background(), "entity-1", concurrencyRuleID)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	cfg := testAlertMutationConfig("http://unused")
	ctx := utils.DeadlineContext(t, time.Second)
	patch, _, err := NewPatchAlertHandler(http.DefaultClient, cfg)(ctx, nil, PatchAlertArgs{EntityID: "entity-1", ID: concurrencyRuleID})
	if err != nil || !strings.Contains(resultText(patch), "at least one of") {
		t.Fatalf("patch_alert: result=%s err=%v", resultText(patch), err)
	}
	update, _, err := NewUpdateAlertHandler(http.DefaultClient, cfg)(ctx, nil, UpdateAlertArgs{EntityID: "entity-1", ID: concurrencyRuleID})
	if err != nil || !update.IsError {
		t.Fatalf("update_alert: result=%s err=%v", resultText(update), err)
	}
}

func TestPatchAlertWaitingOnHeldRuleStopsAtDeadlineWithoutRequests(t *testing.T) {
	unlock, err := lockAlertRule(context.Background(), "ENTITY-1", strings.ToUpper(concurrencyRuleID))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	disabled := true
	_, _, err = NewPatchAlertHandler(server.Client(), testAlertMutationConfig(server.URL))(ctx, nil,
		PatchAlertArgs{EntityID: "entity-1", ID: concurrencyRuleID, IsDisabled: &disabled})
	if err == nil || !strings.Contains(err.Error(), "nothing was changed") {
		t.Fatalf("err = %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}
