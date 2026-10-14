package alerting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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
	for _, args := range []ValidateAlertExpressionArgs{{Expression: "x"}, {EntityID: "e"}} {
		result, _, err := h(context.Background(), nil, args)
		if err != nil || !result.IsError {
			t.Fatalf("args=%+v result=%+v err=%v", args, result, err)
		}
	}
}
