package alerting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func ts(sec int64) TimestampArg { return TimestampArg(strconv.FormatInt(sec, 10)) }

func sampledTimestamps(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Query().Get("timestamp"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"alert_rules":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := slices.Clone(seen)
		slices.Sort(out)
		return out
	}
}

func TestAlertRuleStateHandler_NormalisesTimeFormats(t *testing.T) {
	cases := []struct {
		name       string
		start, end TimestampArg
	}{
		{"epoch seconds", "1718000000", "1718000060"},
		{"epoch milliseconds", "1718000000000", "1718000060000"},
		{"epoch microseconds", "1718000000000000", "1718000060000000"},
		{"epoch nanoseconds", "1718000000000000000", "1718000060000000000"},
		{"RFC3339", "2024-06-10T06:13:20Z", "2024-06-10T06:14:20Z"},
		{"RFC3339 with offset", "2024-06-10T11:43:20+05:30", "2024-06-10T11:44:20+05:30"},
		{"space separated UTC", "2024-06-10 06:13:20", "2024-06-10 06:14:20"},
		{"surrounding spaces", " 1718000000 ", " 2024-06-10T06:14:20Z "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, seen := sampledTimestamps(t)
			handler := NewAlertRuleStateHandler(srv.Client(), newAlertRuleStateTestConfig(srv.URL))
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: tc.start, EndTime: tc.end, Step: 60})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.IsError {
				t.Fatalf("unexpected IsError: %s", result.Content[0].(*mcp.TextContent).Text)
			}
			if got, want := seen(), []string{"1718000000", "1718000060"}; !slices.Equal(got, want) {
				t.Errorf("sampled timestamps = %v, want %v", got, want)
			}
		})
	}
}

func TestAlertRuleStateHandler_RejectsInvalidTimesLocally(t *testing.T) {
	cases := []struct {
		name       string
		start, end TimestampArg
		want       []string
	}{
		{"missing start", "", "1718000060", []string{"start_time is required", "1718000000", "2024-06-10T06:13:20Z"}},
		{"missing end", "1718000000", "  ", []string{"end_time is required"}},
		{"relative phrase", "now-1h", "1718000060", []string{`start_time "now-1h" is not a timestamp`, "Unix epoch seconds", "RFC3339", "get_current_time"}},
		{"date only", "1718000000", "2024-06-10", []string{`end_time "2024-06-10" is not a timestamp`}},
		{"fractional seconds", "1718000000.5", "1718000060", []string{`start_time "1718000000.5" is not a timestamp`}},
		{"duration instead of time", "3600", "1718000060", []string{`start_time "3600" is before 2000-01-01`, "not a duration", "get_current_time"}},
		{"zero", "0", "1718000060", []string{`start_time "0" is before 2000-01-01`}},
		{"negative", "-1718000000", "1718000060", []string{`start_time "-1718000000" is before 2000-01-01`}},
		{"ISO before 2000", "1999-12-31T23:59:59Z", "1718000060", []string{"is before 2000-01-01"}},
		{"beyond int64", "99999999999999999999", "1718000060", []string{`start_time "99999999999999999999" is not a timestamp`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits int64
			upstream := countUpstream(&hits)
			defer upstream.Close()
			handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: tc.start, EndTime: tc.end, Step: 60})
			if err != nil {
				t.Fatalf("expected IsError result, got error: %v", err)
			}
			if !result.IsError {
				t.Fatalf("expected IsError, got %+v", result.Content)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("message %q lacks %q", text, want)
				}
			}
			if hits != 0 {
				t.Errorf("made %d upstream calls for invalid input, want 0", hits)
			}
		})
	}
}

func TestAlertRuleStateHandler_EarliestAcceptedTime(t *testing.T) {
	handler := NewAlertRuleStateHandler(http.DefaultClient, newAlertRuleStateTestConfig("http://unused"))
	result, _, _ := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(946684799), EndTime: ts(946684800), Step: 1})
	if !result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, "before 2000-01-01") {
		t.Fatalf("946684799 should be rejected as before 2000-01-01, got %+v", result.Content)
	}

	srv, seen := sampledTimestamps(t)
	handler = NewAlertRuleStateHandler(srv.Client(), newAlertRuleStateTestConfig(srv.URL))
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(946684800), EndTime: ts(946684801), Step: 1})
	if err != nil || result.IsError {
		t.Fatalf("946684800 (2000-01-01) should be accepted, got err=%v result=%+v", err, result.Content)
	}
	if got := seen(); len(got) != 2 {
		t.Errorf("sampled %v, want 2 timestamps", got)
	}
}

func TestAlertRuleStateInputSchema_AcceptsIntegerOrString(t *testing.T) {
	schema := AlertRuleStateInputSchema()
	for _, name := range []string{"start_time", "end_time"} {
		prop := schema.Properties[name]
		if prop == nil {
			t.Fatalf("schema has no %q", name)
		}
		if !slices.Equal(prop.Types, []string{"integer", "string"}) {
			t.Errorf("%s types = %v, want [integer string]", name, prop.Types)
		}
		if !strings.HasPrefix(prop.Description, "(Required)") {
			t.Errorf("%s description = %q", name, prop.Description)
		}
		if !slices.Contains(schema.Required, name) {
			t.Errorf("%s is not required", name)
		}
	}
	if schema.AdditionalProperties == nil {
		t.Error("schema lost additionalProperties: false")
	}
}

func pinAlertRuleStateClock(t *testing.T, now time.Time) {
	t.Helper()
	t.Cleanup(func() { t.Setenv(utils.FakeNowEnv, ""); _, _ = utils.InitClock() })
	t.Setenv(utils.FakeNowEnv, now.Format(time.RFC3339))
	if _, err := utils.InitClock(); err != nil {
		t.Fatal(err)
	}
}

func TestAlertRuleStateHandler_RejectsFutureSampleBeforePolling(t *testing.T) {
	now := time.Date(2024, 6, 10, 12, 0, 0, 0, time.UTC)
	pinAlertRuleStateClock(t, now)

	// Another call holds all of its polling slots; the rejection must not wait on any.
	busy, arrived := hangingUpstream(t)
	busyCtx, stopBusy := context.WithCancel(context.Background())
	busyDone := make(chan struct{})
	go func() {
		defer close(busyDone)
		NewAlertRuleStateHandler(busy.Client(), newAlertRuleStateTestConfig(busy.URL))(busyCtx, &mcp.CallToolRequest{},
			AlertRuleStateRequest{StartTime: ts(1718000000), EndTime: ts(1718000000 + 19*60), Step: 60})
	}()
	t.Cleanup(func() { stopBusy(); <-busyDone })
	for range alertRuleStateConcurrency {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("busy call never filled its polling slots")
		}
	}

	var hits int64
	upstream := countUpstream(&hits)
	defer upstream.Close()
	handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))
	start, end := now.Add(-time.Minute).Unix(), now.Add(time.Minute).Unix()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(start), EndTime: ts(end), Step: 60})
	if err != nil {
		t.Fatalf("expected IsError result, got error: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected IsError, got %+v", result.Content)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	for _, want := range []string{
		"sample at 1718020860 (2024-06-10T12:01:00Z) is in the future",
		"current time is 1718020800 (2024-06-10T12:00:00Z)",
		"set end_time to 1718020800 or earlier",
		"get_current_time",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("message %q lacks %q", text, want)
		}
	}
	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Errorf("made %d upstream calls for a future sample, want 0", got)
	}
}

// Moving only end_time back to now would leave end_time <= start_time, so the
// remedy must move start_time as well.
func TestAlertRuleStateHandler_FutureStartRemedyMovesStartTime(t *testing.T) {
	now := time.Date(2024, 6, 10, 12, 0, 0, 0, time.UTC)
	pinAlertRuleStateClock(t, now)

	var hits int64
	upstream := countUpstream(&hits)
	defer upstream.Close()
	handler := NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL))
	for name, start := range map[string]time.Time{
		"start at now":    now,
		"start after now": now.Add(time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			end := start.Add(2 * time.Minute)
			result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(start.Unix()), EndTime: ts(end.Unix()), Step: 60})
			if err != nil || !result.IsError {
				t.Fatalf("expected IsError result, got err=%v result=%+v", err, result)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if want := "set start_time before 1718020800 and end_time to 1718020800 or earlier"; !strings.Contains(text, want) {
				t.Errorf("message %q lacks %q", text, want)
			}
		})
	}
	if got := atomic.LoadInt64(&hits); got != 0 {
		t.Errorf("made %d upstream calls for a future window, want 0", got)
	}
}

func TestAlertRuleStateHandler_AcceptsFutureEndWhenLastSampleIsNow(t *testing.T) {
	now := time.Date(2024, 6, 10, 12, 0, 0, 0, time.UTC)
	pinAlertRuleStateClock(t, now)

	srv, seen := sampledTimestamps(t)
	handler := NewAlertRuleStateHandler(srv.Client(), newAlertRuleStateTestConfig(srv.URL))
	start, end := now.Add(-time.Minute).Unix(), now.Add(30*time.Second).Unix()
	result, _, err := handler(context.Background(), &mcp.CallToolRequest{}, AlertRuleStateRequest{StartTime: ts(start), EndTime: ts(end), Step: 60})
	if err != nil || result.IsError {
		t.Fatalf("unexpected failure: err=%v result=%+v", err, result)
	}
	if got, want := seen(), []string{"1718020740", "1718020800"}; !slices.Equal(got, want) {
		t.Errorf("sampled timestamps = %v, want %v", got, want)
	}
}

// Through the real SDK pipeline, so schema validation and the typed decode both
// see the arguments as a model sends them.
func TestAlertRuleStateHandler_AcceptsISOAndNumbersThroughTransport(t *testing.T) {
	var hits int64
	upstream := countUpstream(&hits)
	defer upstream.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "s", Version: "v"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_alert_rule_state", Description: "x", InputSchema: AlertRuleStateInputSchema()},
		NewAlertRuleStateHandler(upstream.Client(), newAlertRuleStateTestConfig(upstream.URL)))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "v"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	for _, args := range []string{
		`{"start_time":"2024-06-10T06:13:20Z","end_time":"2024-06-10T06:14:20Z","step":60}`,
		`{"start_time":1718000000000,"end_time":1718000060,"step":60}`,
		`{"start_time":"1718000000","end_time":1718000060,"step":60}`,
	} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_alert_rule_state", Arguments: json.RawMessage(args)})
		if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		if res.IsError {
			t.Errorf("%s: unexpected IsError: %s", args, res.Content[0].(*mcp.TextContent).Text)
		}
	}
	if got := atomic.LoadInt64(&hits); got != 6 {
		t.Errorf("upstream calls = %d, want 6 (2 per call)", got)
	}
}
