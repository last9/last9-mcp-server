package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestInitProviders_SetsTextMapPropagator(t *testing.T) {
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
