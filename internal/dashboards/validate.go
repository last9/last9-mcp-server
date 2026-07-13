package dashboards

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// maxSnapshotExpiresAtSkew rejects clearly non-second timestamps (e.g. JS Date.now() ms).
// Unix seconds for year ~33658 is still below 1e12; milliseconds today are ~1.7e12.
const maxSnapshotExpiresAtUnixSeconds = int64(1_000_000_000_000)

func validateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("id is required")
	}
	return nil
}

func validateDashboardRequest(dashboard json.RawMessage) error {
	return validateJSONObject("dashboard", dashboard, true)
}

func validateMetadata(metadata json.RawMessage) error {
	if len(metadata) > 0 && !json.Valid(metadata) {
		return errors.New("metadata must be valid JSON")
	}
	return nil
}

func marshalDashboardRequest(req DashboardRequest) ([]byte, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	return payload, nil
}

func mapStatusAPIError(err error, forbiddenMsg, notFoundMsg string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "status 403"):
		return fmt.Errorf("%s: %w", forbiddenMsg, err)
	case strings.Contains(msg, "status 404"):
		return fmt.Errorf("%s: %w", notFoundMsg, err)
	default:
		return err
	}
}

func mapDashboardAPIError(err error) error {
	return mapStatusAPIError(err, "dashboard is readonly and cannot be modified", "dashboard not found")
}

func mapSnapshotAPIError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// Preserve upstream "dashboard not found" from create/list when dashboard_id is bad.
	if strings.Contains(msg, "status 404") && strings.Contains(strings.ToLower(msg), "dashboard not found") {
		return fmt.Errorf("dashboard not found: %w", err)
	}
	return mapStatusAPIError(err,
		"not permitted to access dashboard snapshots",
		"dashboard snapshot not found",
	)
}

func dashboardIDFromResponse(body []byte) string {
	var resp struct {
		Dashboard struct {
			ID string `json:"id"`
		} `json:"dashboard"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	return resp.Dashboard.ID
}

func snapshotIDFromResponse(body []byte) string {
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	return resp.ID
}

func validateJSONObject(field string, raw json.RawMessage, required bool) error {
	if len(raw) == 0 {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if !json.Valid(raw) {
		return fmt.Errorf("%s must be valid JSON", field)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "null" || (len(trimmed) > 0 && trimmed[0] != '{') {
		return fmt.Errorf("%s must be a JSON object", field)
	}
	return nil
}

func validateSnapshotTimeRange(raw json.RawMessage) error {
	if err := validateJSONObject("time_range", raw, true); err != nil {
		return err
	}
	var tr struct {
		From *int64 `json:"from"`
		To   *int64 `json:"to"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return errors.New("time_range must be a JSON object with from/to Unix seconds")
	}
	if tr.From == nil || tr.To == nil {
		return errors.New("time_range.from and time_range.to are required Unix seconds")
	}
	if *tr.From >= *tr.To {
		return errors.New("time_range.from must be less than time_range.to")
	}
	return nil
}

func validateExpiresAt(expiresAt *int64) error {
	if expiresAt == nil {
		return nil
	}
	now := time.Now().Unix()
	if *expiresAt <= now {
		return errors.New("expires_at must be in the future")
	}
	if *expiresAt >= maxSnapshotExpiresAtUnixSeconds {
		return errors.New("expires_at must be Unix seconds, not milliseconds")
	}
	return nil
}

func validateCreateSnapshotArgs(args CreateDashboardSnapshotArgs) error {
	if strings.TrimSpace(args.DashboardID) == "" {
		return errors.New("dashboard_id is required")
	}
	if strings.TrimSpace(args.Name) == "" {
		return errors.New("name is required")
	}
	if err := validateSnapshotTimeRange(args.TimeRange); err != nil {
		return err
	}
	if err := validateJSONObject("dashboard_definition", args.DashboardDefinition, true); err != nil {
		return err
	}
	if err := validateJSONObject("panel_data", args.PanelData, true); err != nil {
		return err
	}
	if err := validateJSONObject("variables", args.Variables, false); err != nil {
		return err
	}
	return validateExpiresAt(args.ExpiresAt)
}
