package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestInitProviders_SetsTextMapPropagator(t *testing.T) {
	// InitProviders' shutdown force-flushes exporters over the network; point
	// it at a local server instead of the real collector so the test doesn't
	// depend on outbound network access being available.
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Registered before the shutdown cleanup below so it runs after it
	// (t.Cleanup is LIFO): the server must still be up when shutdown flushes.
	t.Cleanup(collector.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)

	prevPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTextMapPropagator(prevPropagator)
	})

	ctx := context.Background()
	shutdown, err := InitProviders(ctx, "test", "test-tenant", "test-cluster")
	if err != nil {
		t.Fatalf("InitProviders: %v", err)
	}
	t.Cleanup(func() {
		if err := shutdown(context.Background()); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})

	tr := otel.Tracer("telemetry-setup-test")
	ctx, span := tr.Start(ctx, "probe")
	defer span.End()

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	if got := carrier.Get("traceparent"); got == "" {
		t.Fatal("expected InitProviders to register a TextMapPropagator that injects a non-empty traceparent")
	}
}
