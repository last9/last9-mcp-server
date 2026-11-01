package prom

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"last9-mcp/internal/last9api"
)

func TestNameDiscoveryCallerDeadline(t *testing.T) {
	for name, call := range map[string]func(context.Context, *last9api.Client) error{
		"services": func(ctx context.Context, c *last9api.Client) error {
			_, err := DiscoverServiceNames(ctx, c, QueryInstantInput{Query: "inventory"})
			return err
		},
		"labels": func(ctx context.Context, c *last9api.Client) error {
			_, err := DiscoverLabelNames(ctx, c, GetAPMLabelsInput{Metric: "requests"})
			return err
		},
		"metrics": func(ctx context.Context, c *last9api.Client) error {
			_, err := DiscoverLabelValues(ctx, c, GetLabelValuesInput{Label: "__name__"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
			})
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			if err := call(ctx, client); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("discovery error=%v", err)
			}
		})
	}
}
