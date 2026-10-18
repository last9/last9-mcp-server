package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func callCurrentTime(t *testing.T, now time.Time, args GetCurrentTimeArgs) (map[string]any, error) {
	t.Helper()
	res, _, err := NewGetCurrentTimeHandler(fixedClock(now))(context.Background(), &mcp.CallToolRequest{}, args)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &out); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	return out, nil
}

func TestGetCurrentTime(t *testing.T) {
	now := time.Date(2026, 10, 16, 19, 0, 0, 0, time.UTC)

	t.Run("empty range returns only now", func(t *testing.T) {
		out, err := callCurrentTime(t, now, GetCurrentTimeArgs{})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{
			"now":      map[string]any{"iso": "2026-10-16T19:00:00Z", "epoch_seconds": float64(1792177200)},
			"timezone": "UTC",
		}
		assertJSONEqual(t, out, want)
	})

	t.Run("range in a timezone", func(t *testing.T) {
		out, err := callCurrentTime(t, now, GetCurrentTimeArgs{Range: "since yesterday 9am", Timezone: "IST"})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]any{
			"now":      map[string]any{"iso": "2026-10-16T19:00:00Z", "epoch_seconds": float64(1792177200), "local": "2026-10-17T00:30:00+05:30"},
			"timezone": "Asia/Kolkata",
			"range":    "since yesterday 9am",
			"start":    map[string]any{"iso": "2026-10-16T03:30:00Z", "epoch_seconds": float64(1792121400), "local": "2026-10-16T09:00:00+05:30"},
			"end":      map[string]any{"iso": "2026-10-16T19:00:00Z", "epoch_seconds": float64(1792177200), "local": "2026-10-17T00:30:00+05:30"},
		}
		assertJSONEqual(t, out, want)
	})

	t.Run("relative range in UTC", func(t *testing.T) {
		out, err := callCurrentTime(t, now, GetCurrentTimeArgs{Range: "last 2h"})
		if err != nil {
			t.Fatal(err)
		}
		start := out["start"].(map[string]any)
		if start["iso"] != "2026-10-16T17:00:00Z" || start["epoch_seconds"] != float64(1792170000) {
			t.Fatalf("start = %v", start)
		}
		if _, ok := start["local"]; ok {
			t.Fatalf("UTC result must not repeat the time as local: %v", start)
		}
	})

	for _, tt := range []struct {
		name    string
		args    GetCurrentTimeArgs
		wantErr string
	}{
		{name: "unknown timezone", args: GetCurrentTimeArgs{Timezone: "Mars/Olympus"}, wantErr: `unknown timezone "Mars/Olympus"`},
		{name: "unknown timezone with range", args: GetCurrentTimeArgs{Range: "last 2h", Timezone: "Mars/Olympus"}, wantErr: `unknown timezone "Mars/Olympus"`},
		{name: "unparseable range", args: GetCurrentTimeArgs{Range: "next week"}, wantErr: "unsupported range"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := callCurrentTime(t, now, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func assertJSONEqual(t *testing.T, got, want any) {
	t.Helper()
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Fatalf("got  %s\nwant %s", g, w)
	}
}
