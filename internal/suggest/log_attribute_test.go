package suggest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"last9-mcp/internal/auth"
	"last9-mcp/internal/constants"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeLogAttributeAPI struct {
	sampleBody string
	rejectBody string
	mu         sync.Mutex
	paths      []string
	pipelines  []string
}

func (f *fakeLogAttributeAPI) serve(t *testing.T, fields []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		discovery := strings.Contains(r.URL.Path, constants.EndpointLogsSeries) || strings.HasSuffix(r.URL.Path, "/logs/api/v1/labels")
		switch {
		case discovery && f.rejectBody != "":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(f.rejectBody))
		case strings.Contains(r.URL.Path, constants.EndpointLogsSeries):
			var body struct {
				Pipeline json.RawMessage `json:"pipeline"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode series body: %v", err)
			}
			f.mu.Lock()
			f.pipelines = append(f.pipelines, string(body.Pipeline))
			f.mu.Unlock()
			entry := map[string]string{}
			for _, field := range fields {
				entry[field] = "x"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]string{entry}})
		case strings.HasSuffix(r.URL.Path, "/logs/api/v1/labels"):
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": fields})
		case f.sampleBody != "":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{
				"resultType": "streams",
				"result":     []any{map[string]any{"stream": map[string]string{}, "values": [][]string{{"1", f.sampleBody}}}},
			}})
		default:
			http.Error(w, "no sample", http.StatusServiceUnavailable)
		}
	}))
}

func runLogAttributeDidYouMean(t *testing.T, srv *httptest.Server, args DidYouMeanArgs) (string, error) {
	t.Helper()
	cfg := models.Config{APIBaseURL: srv.URL, Region: "ap-south-1"}
	cfg.TokenManager = &auth.TokenManager{AccessToken: "test-token", ExpiresAt: time.Now().Add(24 * time.Hour)}
	result, _, err := NewDidYouMeanHandler(srv.Client(), cfg)(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return "", err
	}
	return utils.GetTextContent(t, result), nil
}

func TestDidYouMeanLogAttribute_RanksServiceAttributesByWords(t *testing.T) {
	api := &fakeLogAttributeAPI{sampleBody: `{"transaction_ref":"r-1","udf2":"x"}`}
	srv := api.serve(t, []string{"service", "trackid", "udf1", "txn_id", "resource_host_name"})
	defer srv.Close()

	text, err := runLogAttributeDidYouMean(t, srv, DidYouMeanArgs{Query: "transaction id", Type: "log_attribute", Service: "checkout"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Did you mean one of these for \"transaction id\"?\n\n" +
		"  - txn_id (90% match, type: log_attribute, filter_field: attributes['txn_id'], source: indexed)\n" +
		"  - transaction_ref (50% match, type: log_attribute, filter_field: attributes['transaction_ref'], source: body)\n" +
		"  - trackid (25% match, type: log_attribute, filter_field: attributes['trackid'], source: indexed)\n"
	if text != want {
		t.Errorf("response mismatch\ngot:\n%s\nwant:\n%s", text, want)
	}
	if len(api.pipelines) != 1 || !strings.Contains(api.pipelines[0], `"$eq":["ServiceName","checkout"]`) {
		t.Errorf("series call not scoped to the service: %q", api.pipelines)
	}
	for _, p := range api.paths {
		if p == constants.EndpointSuggest {
			t.Errorf("log_attribute must not call the entity suggest API")
		}
	}
}

func TestDidYouMeanLogAttribute_WithoutServiceUsesIndexedLabels(t *testing.T) {
	api := &fakeLogAttributeAPI{}
	srv := api.serve(t, []string{"resource_service_name", "txn_id", "udf1"})
	defer srv.Close()

	text, err := runLogAttributeDidYouMean(t, srv, DidYouMeanArgs{Query: "service name", Type: "log_attribute"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Did you mean one of these for \"service name\"?\n\n" +
		"  - resource_service_name (67% match, type: log_attribute, filter_field: resources['service_name'], source: indexed)\n"
	if text != want {
		t.Errorf("response mismatch\ngot:\n%s\nwant:\n%s", text, want)
	}
}

func TestWordCredit(t *testing.T) {
	tests := []struct {
		a, b string
		want float64
	}{
		{"id", "id", 1},
		{"transaction", "txn", 0.8},
		{"transfer", "xfer", 0.8},
		{"message", "msg", 0.8},
		{"request", "req", 0.8},
		{"id", "trackid", 0.5},
		{"transaction", "trackid", 0},
		{"transaction", "t", 0},
		{"msg", "gsm", 0},
	}
	for _, tt := range tests {
		if got := wordCredit(tt.a, tt.b); got != tt.want {
			t.Errorf("wordCredit(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
		if got := wordCredit(tt.b, tt.a); got != tt.want {
			t.Errorf("wordCredit(%q, %q) = %v, want %v", tt.b, tt.a, got, tt.want)
		}
	}
}

func TestDidYouMeanLogAttribute_NoResemblingAttribute(t *testing.T) {
	srv := (&fakeLogAttributeAPI{}).serve(t, []string{"udf1", "udf2"})
	defer srv.Close()

	text, err := runLogAttributeDidYouMean(t, srv, DidYouMeanArgs{Query: "transaction id", Type: "log_attribute", Service: "checkout"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `No log attributes of service "checkout" resemble "transaction id" in this window. Widen the window, or check the service name.`
	if text != want {
		t.Errorf("response mismatch\ngot:\n%s\nwant:\n%s", text, want)
	}
}

// did_you_mean is served in toolsets that lack the log attribute tools, so the
// no-service miss names the service argument, not another tool.
func TestDidYouMeanLogAttribute_NoResemblingIndexedAttribute(t *testing.T) {
	srv := (&fakeLogAttributeAPI{}).serve(t, []string{"udf1", "udf2"})
	defer srv.Close()

	text, err := runLogAttributeDidYouMean(t, srv, DidYouMeanArgs{Query: "transaction id", Type: "log_attribute"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `No indexed log attributes resemble "transaction id" in this window. Pass service to include that service's body-derived attributes, or widen the window.`
	if text != want {
		t.Errorf("response mismatch\ngot:\n%s\nwant:\n%s", text, want)
	}
}

func TestDidYouMeanLogAttribute_NegativeLookbackRejected(t *testing.T) {
	api := &fakeLogAttributeAPI{}
	srv := api.serve(t, nil)
	defer srv.Close()

	_, err := runLogAttributeDidYouMean(t, srv, DidYouMeanArgs{Query: "txn", Type: "log_attribute", LookbackMinutes: -5})
	if err == nil || !strings.Contains(err.Error(), "lookback_minutes must be at least 1, got -5") {
		t.Errorf("want negative lookback rejection, got %v", err)
	}
	if len(api.paths) != 0 {
		t.Errorf("rejected call reached the API: %v", api.paths)
	}
}

func TestDidYouMean_LogAttributeScopeRejectedForOtherTypes(t *testing.T) {
	for _, args := range []DidYouMeanArgs{
		{Query: "checkout", Service: "checkout"},
		{Query: "checkout", Type: "service", LookbackMinutes: 60},
		{Query: "checkout", Type: "service", StartTimeISO: "2026-11-24T10:00:00Z"},
	} {
		api := &fakeLogAttributeAPI{}
		srv := api.serve(t, nil)
		_, err := runLogAttributeDidYouMean(t, srv, args)
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "only apply to type \"log_attribute\"") {
			t.Errorf("args %+v: want scope rejection, got %v", args, err)
		}
		if len(api.paths) != 0 {
			t.Errorf("args %+v: rejected call reached the API: %v", args, api.paths)
		}
	}
}

func TestDidYouMeanLogAttribute_UpstreamRejectionIsSanitized(t *testing.T) {
	for _, service := range []string{"", "checkout"} {
		api := &fakeLogAttributeAPI{rejectBody: `{"error":"bad window","auth":"Bearer test-secret","upstream":"http://logs-reader.internal:9000/v2/series"}`}
		srv := api.serve(t, nil)
		_, err := runLogAttributeDidYouMean(t, srv, DidYouMeanArgs{Query: "txn", Type: "log_attribute", Service: service})
		srv.Close()
		if err == nil {
			t.Fatalf("service %q: want an error for an upstream 400", service)
		}
		msg := err.Error()
		for _, leaked := range []string{"test-secret", "logs-reader.internal"} {
			if strings.Contains(msg, leaked) {
				t.Errorf("service %q: error leaks %q: %s", service, leaked, msg)
			}
		}
		for _, kept := range []string{"HTTP 400", "bad window", "[redacted-credential]", "[redacted-url]"} {
			if !strings.Contains(msg, kept) {
				t.Errorf("service %q: error lacks %q: %s", service, kept, msg)
			}
		}
	}
}
