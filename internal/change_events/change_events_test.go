package change_events

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// jsonParam reports whether a struct exposes a JSON property `name`.
func jsonParam(rt reflect.Type, name string) (present bool) {
	for i := 0; i < rt.NumField(); i++ {
		if strings.Split(rt.Field(i).Tag.Get("json"), ",")[0] == name {
			return true
		}
	}
	return false
}

// Integration test for get_change_events tool
func TestGetChangeEventsHandler_Integration(t *testing.T) {
	cfg := utils.SetupTestConfigOrSkip(t)

	handler := NewGetChangeEventsHandler(http.DefaultClient, *cfg)

	tests := []struct {
		name string
		args GetChangeEventsArgs
	}{
		{
			name: "Get change events with default parameters",
			args: GetChangeEventsArgs{
				LookbackMinutes: 60,
			},
		},
		{
			name: "Get change events with service filter",
			args: GetChangeEventsArgs{
				LookbackMinutes: 30,
				ServiceName:     "test-service",
			},
		},
		{
			name: "Get change events with environment filter",
			args: GetChangeEventsArgs{
				LookbackMinutes: 60,
				Env:             "prod",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			req := &mcp.CallToolRequest{}
			result, _, err := handler(ctx, req, tt.args)

			if utils.CheckAPIError(t, err) {
				return
			}

			text := utils.GetTextContent(t, result)

			var response map[string]interface{}
			if err := json.Unmarshal([]byte(text), &response); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			count := 0
			if changeEvents, ok := response["change_events"].([]interface{}); ok {
				count = len(changeEvents)
			}
			availableEventNames := 0
			if eventNames, ok := response["available_event_names"].([]interface{}); ok {
				availableEventNames = len(eventNames)
			}
			t.Logf("Integration test successful: %d change event(s), %d available event name(s)",
				count, availableEventNames)
		})
	}
}

func TestGetChangeEventsArgs_UsesCanonicalNames(t *testing.T) {
	rt := reflect.TypeOf(GetChangeEventsArgs{})
	for _, canon := range []string{"service_name", "env"} {
		if !jsonParam(rt, canon) {
			t.Fatalf("GetChangeEventsArgs must expose canonical param %q", canon)
		}
	}
	for _, legacy := range []string{"service", "environment"} {
		if jsonParam(rt, legacy) {
			t.Fatalf("legacy param %q must be removed", legacy)
		}
	}
}

func TestBuildChangeEventsPromQL(t *testing.T) {
	excludes := backupEventExclude + "," + scheduledSearchExclude
	metric := func(matchers string) string {
		return `last9_change_events{` + matchers + `}`
	}

	unfiltered := buildChangeEventsPromQL("", "", "")
	if unfiltered != metric(excludes) {
		t.Fatalf("unfiltered:\n got %s\nwant %s", unfiltered, metric(excludes))
	}

	envOnly := buildChangeEventsPromQL("", "uat", "")
	wantEnv := metric(excludes+`,deployment_environment="uat"`) + " or " +
		metric(excludes+`,deployment_environment="",env="uat"`)
	if envOnly != wantEnv {
		t.Fatalf("env filter:\n got %s\nwant %s", envOnly, wantEnv)
	}

	svcOnly := buildChangeEventsPromQL("checkout", "", "")
	wantSvc := metric(excludes+`,service="checkout"`) + " or " +
		metric(excludes+`,service="",service_name="checkout"`)
	if svcOnly != wantSvc {
		t.Fatalf("service filter:\n got %s\nwant %s", svcOnly, wantSvc)
	}

	eventOnly := buildChangeEventsPromQL("", "", "deployment")
	wantEvent := metric(excludes+`,event_name="deployment"`) + " or " +
		metric(excludes+`,event_name="",event_type="deployment"`)
	if eventOnly != wantEvent {
		t.Fatalf("event filter:\n got %s\nwant %s", eventOnly, wantEvent)
	}

	both := buildChangeEventsPromQL("checkout", "uat", "")
	wantBoth := strings.Join([]string{
		metric(excludes + `,service="checkout",deployment_environment="uat"`),
		metric(excludes + `,service="checkout",deployment_environment="",env="uat"`),
		metric(excludes + `,service="",service_name="checkout",deployment_environment="uat"`),
		metric(excludes + `,service="",service_name="checkout",deployment_environment="",env="uat"`),
	}, " or ")
	if both != wantBoth {
		t.Fatalf("service+env:\n got %s\nwant %s", both, wantBoth)
	}
}
