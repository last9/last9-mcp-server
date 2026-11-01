package alerting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func validCreateKPIArgs() CreateKPIArgs {
	return CreateKPIArgs{EntityID: "entity-1", Name: "p99_latency", Query: "histogram_quantile(0.99, rate(x_bucket[5m]))", Unit: "seconds"}
}

func TestCreateKPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.EscapedPath() != "/entities/entity-1/kpis/" {
			t.Errorf("request = %s %s", r.Method, r.URL.EscapedPath())
		}
		want := `{"name":"p99_latency","description":"checkout p99","definition":{"query":"histogram_quantile(0.99, rate(x_bucket[5m]))","unit":"seconds"}}`
		if got, _ := io.ReadAll(r.Body); string(got) != want {
			t.Errorf("body = %s", got)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"kpi-9","name":"p99_latency","definition":{"query":"histogram_quantile(0.99, rate(x_bucket[5m]))","unit":"seconds","source":"levitate"}}`))
	}))
	defer server.Close()

	args := validCreateKPIArgs()
	args.Description = "checkout p99"
	result, _, err := NewCreateKPIHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, args)
	if err != nil || result.IsError {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	text := resultText(result)
	wantJSON := `{"id":"kpi-9","name":"p99_latency","query":"histogram_quantile(0.99, rate(x_bucket[5m]))","unit":"seconds"}`
	if !strings.HasPrefix(text, wantJSON) || !strings.Contains(text, "Keep this id") || !strings.Contains(text, "do not call create_kpi again") {
		t.Fatalf("text = %s", text)
	}
}

func TestCreateKPIValidationMakesNoCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()

	for field, mutate := range map[string]func(*CreateKPIArgs){
		"entity_id": func(a *CreateKPIArgs) { a.EntityID = " " },
		"name":      func(a *CreateKPIArgs) { a.Name = "" },
		"query":     func(a *CreateKPIArgs) { a.Query = "\t" },
		"unit":      func(a *CreateKPIArgs) { a.Unit = "" },
	} {
		args := validCreateKPIArgs()
		mutate(&args)
		result, _, err := NewCreateKPIHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, args)
		if err != nil || !result.IsError || !strings.Contains(resultText(result), field) {
			t.Fatalf("%s: result=%+v err=%v", field, result, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("made %d API calls for invalid input", calls.Load())
	}
}

func TestCreateKPIDuplicateNamePointsAtExistingKPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		http.Error(w, `{"error":"kpi name p99_latency already used"}`, http.StatusConflict)
	}))
	defer server.Close()

	result, _, err := NewCreateKPIHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, validCreateKPIArgs())
	if err != nil || !result.IsError || !strings.Contains(resultText(result), "get_entity_kpis") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestCreateKPIRelaysQueryRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		http.Error(w, `{"error":"parse promql query: unexpected token"}`, http.StatusUnprocessableEntity)
	}))
	defer server.Close()

	_, _, err := NewCreateKPIHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, validCreateKPIArgs())
	if err == nil || !strings.Contains(err.Error(), "unexpected token") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateKPIRejectsResponseWithoutID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"p99_latency"}`))
	}))
	defer server.Close()

	_, _, err := NewCreateKPIHandler(server.Client(), testAlertMutationConfig(server.URL))(context.Background(), nil, validCreateKPIArgs())
	if err == nil || !strings.Contains(err.Error(), "missing id") {
		t.Fatalf("err = %v", err)
	}
}
