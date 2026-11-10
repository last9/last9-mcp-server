package alerting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestValidateAlertExpressionHandler(t *testing.T) {
	tests := []struct {
		name   string
		status int
		resp   string
	}{
		{"valid", http.StatusOK, `{"valid":true}`},
		{"invalid expression is a result, not an error", http.StatusUnprocessableEntity, `{"valid":false,"reason":"expression yields 0 timeseries"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.EscapedPath() != "/entities/entity-1/alert-rules/validate_expression" {
					t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
				}
				if got, _ := io.ReadAll(r.Body); string(got) != `{"expression":"x / y","indicators_used":{"x":"k1","y":"k2"}}` {
					t.Errorf("body = %s", got)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.resp))
			}))
			defer server.Close()
			result, _, err := NewValidateAlertExpressionHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
				ValidateAlertExpressionArgs{EntityID: "entity-1", Expression: "x / y", IndicatorsUsed: map[string]string{"x": "k1", "y": "k2"}})
			if err != nil || result.IsError {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if resultText(result) != tt.resp {
				t.Fatalf("text = %s", resultText(result))
			}
		})
	}
}

func TestValidateAlertExpressionRequiresArgs(t *testing.T) {
	h := NewValidateAlertExpressionHandler(http.DefaultClient, testAlertMutationConfig("http://unused"))
	for _, args := range []ValidateAlertExpressionArgs{{Expression: "x"}, {EntityID: "e"}, {EntityID: "e", Expression: "x"}} {
		result, _, err := h(context.Background(), nil, args)
		if err != nil || !result.IsError {
			t.Fatalf("args=%+v result=%+v err=%v", args, result, err)
		}
	}
}

// Saved rules bind KPI names textually, so "Test Alert" is one indicator to create_alert;
// the dry-run's parser splits it into two variables unless it is written as [Test Alert].
func TestValidateAlertExpressionBracketsNamesItCannotParseBare(t *testing.T) {
	tests := []struct {
		name       string
		expression string
		indicators map[string]string
		want       string
	}{
		{"space", "Test Alert", map[string]string{"Test Alert": "k1"}, "[Test Alert]"},
		{"space beside identifier", "Test Alert / errors", map[string]string{"Test Alert": "k1", "errors": "k2"}, "[Test Alert] / errors"},
		{"longest name wins", "Test Alert p99 - Test Alert", map[string]string{"Test Alert": "k1", "Test Alert p99": "k2"}, "[Test Alert p99] - [Test Alert]"},
		{"dot and dash", "http.errors / req-total", map[string]string{"http.errors": "k1", "req-total": "k2"}, "[http.errors] / [req-total]"},
		{"already bracketed", "[Test Alert] * 2", map[string]string{"Test Alert": "k1"}, "[Test Alert] * 2"},
		{"identifier names untouched", "errors / requests", map[string]string{"errors": "k1", "requests": "k2"}, "errors / requests"},
		{"name inside a longer word untouched", "my-kpi2 + 1", map[string]string{"my-kpi": "k1"}, "my-kpi2 + 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent struct {
				Expression     string            `json:"expression"`
				IndicatorsUsed map[string]string `json:"indicators_used"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
					t.Errorf("decode body: %v", err)
				}
				_, _ = w.Write([]byte(`{"valid":true}`))
			}))
			defer server.Close()
			result, _, err := NewValidateAlertExpressionHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil,
				ValidateAlertExpressionArgs{EntityID: "entity-1", Expression: tt.expression, IndicatorsUsed: tt.indicators})
			if err != nil || result.IsError {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if sent.Expression != tt.want {
				t.Errorf("expression = %q, want %q", sent.Expression, tt.want)
			}
			if !reflect.DeepEqual(sent.IndicatorsUsed, tt.indicators) {
				t.Errorf("indicators_used = %v, want %v", sent.IndicatorsUsed, tt.indicators)
			}
		})
	}
}
