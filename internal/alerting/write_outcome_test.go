package alerting

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"last9-mcp/internal/utils"
	"last9-mcp/internal/writefault"
)

// Handlers that turn upstream errors into results must pass unknown write outcomes
// through as errors, so the registration layer can tell the agent how to check.
func TestWriteHandlersPassUnknownOutcomeThrough(t *testing.T) {
	faults := map[string]func(http.ResponseWriter, *http.Request){
		"gateway 502": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
	}
	for _, fault := range writefault.All {
		faults[string(fault)] = func(w http.ResponseWriter, r *http.Request) { writefault.Serve(w, r, fault) }
	}
	calls := map[string]func(*http.Client, string) error{
		"create_alert_from_chart": func(client *http.Client, url string) error {
			_, _, err := NewCreateAlertFromChartHandler(client, testAlertMutationConfig(url))(context.Background(), nil, CreateAlertFromChartArgs{
				Surface: "discover-service", ChartKey: "error_rate", ServiceName: "checkout", SignalKey: "error_rate", Name: "checkout errors",
			})
			return err
		},
		"create_notification_channel": func(client *http.Client, url string) error {
			_, _, err := NewCreateNotificationChannelHandler(client, testAlertMutationConfig(url))(context.Background(), nil, CreateNotificationChannelArgs{
				Name: "oncall", Type: "slack", Destination: "https://hooks.slack.com/services/x",
			})
			return err
		},
	}
	for tool, call := range calls {
		for name, serve := range faults {
			t.Run(tool+"/"+name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(serve))
				defer srv.Close()
				err := call(&http.Client{Timeout: 200 * time.Millisecond}, srv.URL)
				var unknown *utils.WriteOutcomeUnknownError
				if !errors.As(err, &unknown) {
					t.Fatalf("unknown outcome not passed through as an error: %v", err)
				}
			})
		}
	}
}
