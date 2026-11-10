package entities

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateAlertExpressionRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/entities/entity%2F1/alert-rules/validate_expression" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		if got, _ := io.ReadAll(r.Body); string(got) != `{"expression":"x / y","indicators_used":{"x":"kpi-1","y":"kpi-2"}}` {
			t.Errorf("body = %s", got)
		}
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	defer srv.Close()
	raw, err := ValidateAlertExpression(context.Background(), newTestClient(srv), ValidateExpressionInput{
		EntityID: "entity/1", Expression: "x / y", IndicatorsUsed: map[string]string{"x": "kpi-1", "y": "kpi-2"},
	})
	if err != nil || string(raw) != `{"valid":true}` {
		t.Fatalf("raw=%s err=%v", raw, err)
	}
}
