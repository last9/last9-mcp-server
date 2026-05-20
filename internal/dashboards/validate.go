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
	if len(dashboard) == 0 {
		return errors.New("dashboard is required")
	}
	if !json.Valid(dashboard) {
		return errors.New("dashboard must be valid JSON")
	}
	trimmed := strings.TrimSpace(string(dashboard))
	if trimmed == "null" || (len(trimmed) > 0 && trimmed[0] != '{') {
		return errors.New("dashboard must be a JSON object")
	}
	return nil
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

func mapDashboardAPIError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "status 403"):
		return fmt.Errorf("dashboard is readonly and cannot be modified: %w", err)
	case strings.Contains(msg, "status 404"):
		return fmt.Errorf("dashboard not found: %w", err)
	default:
		return err
	}
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
