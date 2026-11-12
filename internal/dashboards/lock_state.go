package dashboards

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	apidashboards "last9-mcp/internal/last9api/dashboards"
	"last9-mcp/internal/utils"
)

// The API calls a locked dashboard "readonly"; tools serve it as "locked".
type dashboardLockFields struct {
	Readonly  *bool   `json:"readonly"`
	CreatedBy *string `json:"created_by"`
}

// withDashboardLockState adds top-level locked and created_by to a get-dashboard
// body. A body that is not a {dashboard} object is served unchanged.
func withDashboardLockState(body []byte) []byte {
	body = normalizeEscapedHTML(body)
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil || resp == nil {
		return body
	}
	var fields dashboardLockFields
	if err := json.Unmarshal(resp["dashboard"], &fields); err != nil {
		return body
	}
	resp["locked"] = encodeJSON(fields.Readonly != nil && *fields.Readonly)
	resp["created_by"] = encodeJSON(fields.CreatedBy)
	return encodeJSON(resp)
}

func normalizeEscapedHTML(body []byte) []byte {
	lower := bytes.ToLower(body)
	if !bytes.Contains(lower, []byte(`\u003c`)) && !bytes.Contains(lower, []byte(`\u003e`)) && !bytes.Contains(lower, []byte(`\u0026`)) {
		return body
	}
	var value any
	if err := decodeJSON(body, &value); err != nil {
		return body
	}
	return encodeJSON(value)
}

func withDashboardRowsLocked(body []byte) []byte {
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(body, &rows); err != nil {
		return body
	}
	for _, row := range rows {
		if readonly, ok := row["readonly"]; ok && row != nil {
			row["locked"] = readonly
		}
	}
	return encodeJSON(rows)
}

func encodeJSON(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

func decodeJSON(body []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	return dec.Decode(v)
}

// isDashboardLockedRefusal tells the API's lock refusal from the 403 a built-in dashboard gets.
func isDashboardLockedRefusal(status int, body string) bool {
	return status == http.StatusForbidden && strings.Contains(strings.ToLower(body), "dashboard is locked")
}

func upstreamLockedRefusal(err error) bool {
	var upstream *utils.UpstreamHTTPError
	return errors.As(err, &upstream) && isDashboardLockedRefusal(upstream.StatusCode, upstream.Body)
}

func upstreamBuiltInRefusal(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	var upstream *utils.UpstreamHTTPError
	if errors.As(err, &upstream) {
		text += " " + upstream.Body
	}
	return strings.Contains(strings.ToLower(text), "built-in dashboards are read-only")
}

// dashboardLockedError keeps the "dashboard is locked" prefix stable: clients match on it.
func dashboardLockedError(createdBy string) error {
	if createdBy == "" {
		createdBy = "unknown"
	}
	return fmt.Errorf("dashboard is locked (created_by: %s): only its author or an org admin can unlock it with set_dashboard_lock; nothing was written", createdBy)
}

// lockedDashboardAuthor is best effort: the refusal stands even if the author cannot be read.
func lockedDashboardAuthor(ctx context.Context, api *last9api.Client, id string) string {
	resp, err := apidashboards.GetDashboard(ctx, api, id)
	if err != nil {
		return ""
	}
	createdBy, _ := resp.Dashboard["created_by"].(string)
	return createdBy
}
