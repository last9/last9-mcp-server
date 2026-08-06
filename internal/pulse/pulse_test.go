package pulse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func pulseTestConfig() models.Config {
	return models.Config{
		APIBaseURL: "https://api.example.test",
		TokenManager: &auth.TokenManager{
			AccessToken: "test-token",
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func pulseTestClient(handler func(*http.Request) (int, string)) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status, body := handler(request)
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
}

func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	content, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T, want *mcp.TextContent", result.Content[0])
	}
	return content.Text
}

func TestListRunsPreservesCanonicalPage(t *testing.T) {
	want := `{"runs":[{"id":"run-1","analysis_status":"partial","delivery_status":"failed"}],"next_cursor":"opaque","truncated":true}`
	client := pulseTestClient(func(r *http.Request) (int, string) {
		if r.URL.Path != pulseBasePath+"/runs" || r.URL.Query().Get("limit") != "25" {
			t.Errorf("request URL = %s, want runs page with limit", r.URL.String())
		}
		if r.Header.Get("X-LAST9-API-TOKEN") != "Bearer test-token" {
			t.Errorf("API token header = %q", r.Header.Get("X-LAST9-API-TOKEN"))
		}
		return http.StatusOK, want
	})

	handler := NewListRunsHandler(client, pulseTestConfig())
	result, _, err := handler(context.Background(), nil, ListRunsArgs{})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultText(t, result); got != want {
		t.Fatalf("MCP response changed API record\ngot:  %s\nwant: %s", got, want)
	}
}

func TestListEvidencePreservesPartialMetadata(t *testing.T) {
	want := `{"evidence":[{"id":"e-1","safe_summary":{"transitions":6}}],"next_cursor":"next","truncated":true}`
	client := pulseTestClient(func(r *http.Request) (int, string) {
		if r.URL.Path != pulseBasePath+"/runs/run-1/evidence" || r.URL.Query().Get("cursor") != "prior" {
			t.Errorf("request URL = %s", r.URL.String())
		}
		return http.StatusOK, want
	})

	handler := NewListEvidenceHandler(client, pulseTestConfig())
	result, _, err := handler(context.Background(), nil, RunPageArgs{RunID: "run-1", Limit: 50, Cursor: "prior"})
	if err != nil {
		t.Fatal(err)
	}
	if got := resultText(t, result); got != want {
		t.Fatalf("MCP response changed evidence page\ngot:  %s\nwant: %s", got, want)
	}
}

func TestCreateSubscriptionRequiresConfirmation(t *testing.T) {
	requests := 0
	client := pulseTestClient(func(*http.Request) (int, string) {
		requests++
		return http.StatusOK, `{}`
	})

	handler := NewCreateSubscriptionHandler(client, pulseTestConfig())
	_, _, err := handler(context.Background(), nil, CreateSubscriptionArgs{SubscriptionInput: validSubscriptionInput()})
	if err == nil || !strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("error = %v, want confirmation error", err)
	}
	if requests != 0 {
		t.Fatalf("upstream requests = %d, want 0", requests)
	}
}

func TestUpdateSubscriptionRequiresConfirmation(t *testing.T) {
	requests := 0
	client := pulseTestClient(countPulseRequests(&requests))
	handler := NewUpdateSubscriptionHandler(client, pulseTestConfig())
	args := UpdateSubscriptionArgs{SubscriptionID: "subscription-1", SubscriptionInput: validSubscriptionInput()}
	_, _, err := handler(context.Background(), nil, args)
	assertConfirmationRejected(t, requests, err)
}

func TestEnableSubscriptionRequiresConfirmation(t *testing.T) {
	requests := 0
	client := pulseTestClient(countPulseRequests(&requests))
	handler := NewEnableSubscriptionHandler(client, pulseTestConfig())
	_, _, err := handler(context.Background(), nil, SetSubscriptionEnabledArgs{SubscriptionID: "subscription-1"})
	assertConfirmationRejected(t, requests, err)
}

func TestDisableSubscriptionRequiresConfirmation(t *testing.T) {
	requests := 0
	client := pulseTestClient(countPulseRequests(&requests))
	handler := NewDisableSubscriptionHandler(client, pulseTestConfig())
	_, _, err := handler(context.Background(), nil, SetSubscriptionEnabledArgs{SubscriptionID: "subscription-1"})
	assertConfirmationRejected(t, requests, err)
}

func TestWriteDispositionRequiresConfirmation(t *testing.T) {
	requests := 0
	client := pulseTestClient(countPulseRequests(&requests))
	handler := NewWriteDispositionHandler(client, pulseTestConfig())
	args := WriteDispositionArgs{FindingID: "finding-1", Disposition: "expected"}
	_, _, err := handler(context.Background(), nil, args)
	assertConfirmationRejected(t, requests, err)
}

func countPulseRequests(requests *int) func(*http.Request) (int, string) {
	return func(*http.Request) (int, string) {
		(*requests)++
		return http.StatusOK, `{}`
	}
}

func assertConfirmationRejected(t *testing.T, requests int, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("error = %v, want confirmation error", err)
	}
	if requests != 0 {
		t.Fatalf("upstream requests = %d, want 0", requests)
	}
}

func TestRunIDCannotChangeTheOrganizationScopedPath(t *testing.T) {
	requests := 0
	client := pulseTestClient(func(*http.Request) (int, string) {
		requests++
		return http.StatusOK, `{}`
	})
	handler := NewGetRunHandler(client, pulseTestConfig())
	_, _, err := handler(context.Background(), nil, GetRunArgs{RunID: "../other-org"})
	if err == nil || !strings.Contains(err.Error(), "path characters") {
		t.Fatalf("error = %v, want invalid path error", err)
	}
	if requests != 0 {
		t.Fatalf("upstream requests = %d, want 0", requests)
	}
}

func TestEscapedIDRejectsDotSegments(t *testing.T) {
	for _, id := range []string{".", ".."} {
		t.Run(id, func(t *testing.T) {
			_, err := escapedID(id)
			if err == nil || !strings.Contains(err.Error(), "path characters") {
				t.Fatalf("escapedID(%q) error = %v, want invalid path error", id, err)
			}
		})
	}
}

func TestListFindingsForwardsPathCursorAndLimit(t *testing.T) {
	want := `{"findings":[],"next_cursor":"","truncated":false}`
	client := pulseTestClient(func(r *http.Request) (int, string) {
		if r.URL.Path != pulseBasePath+"/runs/run-1/findings" {
			t.Errorf("request path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "50" || r.URL.Query().Get("cursor") != "prior" {
			t.Errorf("request query = %s", r.URL.RawQuery)
		}
		return http.StatusOK, want
	})
	result, _, err := NewListFindingsHandler(client, pulseTestConfig())(
		context.Background(), nil, RunPageArgs{RunID: "run-1", Limit: 50, Cursor: "prior"})
	if err != nil || resultText(t, result) != want {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestGetFindingUsesOccurrencePath(t *testing.T) {
	client := pulseTestClient(func(r *http.Request) (int, string) {
		if r.URL.Path != pulseBasePath+"/runs/run-1/findings/occurrence-1" {
			t.Errorf("request path = %s", r.URL.Path)
		}
		return http.StatusOK, `{"finding":{"id":"occurrence-1"}}`
	})
	result, _, err := NewGetFindingHandler(client, pulseTestConfig())(
		context.Background(), nil, GetFindingArgs{RunID: "run-1", OccurrenceID: "occurrence-1"})
	if err != nil || !strings.Contains(resultText(t, result), "occurrence-1") {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestCreateSubscriptionKeepsConfigurationSeparateFromEnablement(t *testing.T) {
	client := pulseTestClient(assertCreateSubscriptionRequest(t))

	handler := NewCreateSubscriptionHandler(client, pulseTestConfig())
	args := CreateSubscriptionArgs{SubscriptionInput: validSubscriptionInput(), Confirmed: true}
	result, _, err := handler(context.Background(), nil, args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resultText(t, result), `"enabled":false`) {
		t.Fatalf("response = %s", resultText(t, result))
	}
}

func assertCreateSubscriptionRequest(t *testing.T) func(*http.Request) (int, string) {
	t.Helper()
	return func(r *http.Request) (int, string) {
		if r.Method != http.MethodPost || r.URL.Path != pulseBasePath+"/subscriptions" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"confirmed", "enabled", "organization_id"} {
			if _, exists := body[forbidden]; exists {
				t.Errorf("request body contains %q: %v", forbidden, body)
			}
		}
		return http.StatusOK, `{"id":"sub-1","enabled":false}`
	}
}

func TestWriteDispositionPreservesStaleConflict(t *testing.T) {
	client := pulseTestClient(func(*http.Request) (int, string) {
		return http.StatusConflict, `{"error":"pulse disposition is stale; re-read before updating"}`
	})

	handler := NewWriteDispositionHandler(client, pulseTestConfig())
	args := WriteDispositionArgs{FindingID: "finding-1", ExpectedVersion: 2, Disposition: "expected", Confirmed: true}
	_, _, err := handler(context.Background(), nil, args)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("error = %#v, want preserved HTTP 409", err)
	}
	if !strings.Contains(apiErr.Body, "re-read") {
		t.Fatalf("error body = %q, want upstream guidance", apiErr.Body)
	}
}

func TestWriteDispositionSendsOnlyCanonicalFields(t *testing.T) {
	client := pulseTestClient(func(r *http.Request) (int, string) {
		if r.Method != http.MethodPatch || r.URL.Path != pulseBasePath+"/findings/finding-1/disposition" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"expected_version": float64(2), "disposition": "investigate", "rationale": "needs owner review"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request body = %#v, want %#v", got, want)
		}
		return http.StatusOK, `{"version":3,"disposition":"investigate"}`
	})

	handler := NewWriteDispositionHandler(client, pulseTestConfig())
	args := WriteDispositionArgs{FindingID: "finding-1", ExpectedVersion: 2, Disposition: "investigate", Rationale: "needs owner review", Confirmed: true}
	if _, _, err := handler(context.Background(), nil, args); err != nil {
		t.Fatal(err)
	}
}

func validSubscriptionInput() SubscriptionInput {
	return SubscriptionInput{
		Name: "Weekly alert review", Schedule: "0 9 * * 1", Timezone: "UTC",
		Recipients: []string{"operator@example.test"}, Scope: map[string]any{"services": []any{"checkout"}},
		Config: map[string]any{"observation_window": "7d"}, ConfigVersion: "pulse-v1",
	}
}
