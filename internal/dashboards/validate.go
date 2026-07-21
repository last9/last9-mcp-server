package dashboards

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

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
