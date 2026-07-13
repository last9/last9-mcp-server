package dashboards

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidateCreateSnapshotArgs_TimeRangeAndExpiresAt(t *testing.T) {
	base := CreateDashboardSnapshotArgs{
		DashboardID:         "dash-1",
		Name:                "Freeze",
		TimeRange:           json.RawMessage(`{"from":100,"to":200}`),
		DashboardDefinition: json.RawMessage(`{"name":"Dash"}`),
		PanelData:           json.RawMessage(`{"p1":[]}`),
	}

	if err := validateCreateSnapshotArgs(base); err != nil {
		t.Fatalf("valid args: %v", err)
	}

	badRange := base
	badRange.TimeRange = json.RawMessage(`{"from":200,"to":100}`)
	if err := validateCreateSnapshotArgs(badRange); err == nil || !strings.Contains(err.Error(), "from must be less") {
		t.Fatalf("inverted range: %v", err)
	}

	missingTo := base
	missingTo.TimeRange = json.RawMessage(`{"from":100}`)
	if err := validateCreateSnapshotArgs(missingTo); err == nil || !strings.Contains(err.Error(), "time_range") {
		t.Fatalf("missing to: %v", err)
	}

	past := time.Now().Add(-time.Hour).Unix()
	expired := base
	expired.ExpiresAt = &past
	if err := validateCreateSnapshotArgs(expired); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("past expires_at: %v", err)
	}

	ms := time.Now().UnixMilli() + 60_000
	millis := base
	millis.ExpiresAt = &ms
	if err := validateCreateSnapshotArgs(millis); err == nil || !strings.Contains(err.Error(), "milliseconds") {
		t.Fatalf("ms expires_at: %v", err)
	}
}
