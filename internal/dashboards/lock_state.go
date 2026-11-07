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
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The API calls a locked dashboard "readonly"; tools serve it as "locked".
type dashboardLockFields struct {
	Readonly  *bool   `json:"readonly"`
	CreatedBy *string `json:"created_by"`
}

// withDashboardLockState adds top-level locked and created_by to a get-dashboard
// body. A body that is not a {dashboard} object is served unchanged.
func withDashboardLockState(body []byte) []byte {
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

// withDashboardRowsLocked adds locked to each list row that reports readonly.
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

// isDashboardLockedRefusal tells the API's lock refusal from the 403 a built-in dashboard gets.
func isDashboardLockedRefusal(status int, body string) bool {
	return status == http.StatusForbidden && strings.Contains(strings.ToLower(body), "dashboard is locked")
}

func upstreamLockedRefusal(err error) bool {
	var upstream *utils.UpstreamHTTPError
	return errors.As(err, &upstream) && isDashboardLockedRefusal(upstream.StatusCode, upstream.Body)
}

func isForbidden(err error) bool {
	var upstream *utils.UpstreamHTTPError
	return errors.As(err, &upstream) && upstream.StatusCode == http.StatusForbidden
}

// dashboardLock is the lock state a dashboard read reports. Readonly stays nil
// when the field is absent or not a bool, so an unknown state never reads as
// unlocked.
type dashboardLock struct {
	Readonly  *bool
	CreatedBy string
}

func dashboardLockOf(dashboard map[string]any) dashboardLock {
	var lock dashboardLock
	if readonly, ok := dashboard["readonly"].(bool); ok {
		lock.Readonly = &readonly
	}
	lock.CreatedBy, _ = dashboard["created_by"].(string)
	return lock
}

func (l dashboardLock) locked() bool {
	return l.Readonly != nil && *l.Readonly
}

// readDashboardLock reads the dashboard a write targets so a locked one is
// refused before any write is sent.
func readDashboardLock(ctx context.Context, client *http.Client, cfg models.Config, id string) (dashboardLock, error) {
	current, err := apidashboards.GetDashboard(ctx, last9api.NewClient(client, cfg), id)
	if err != nil {
		return dashboardLock{}, fmt.Errorf("%w; nothing was written", mapDashboardFetchError(id, err))
	}
	return dashboardLockOf(current.Dashboard), nil
}

// lockedResult is a user error: the caller recovers by asking for an unlock or
// copying the dashboard, not by retrying. Clients match the "dashboard is
// locked" prefix, so keep it stable.
func lockedResult(lock dashboardLock) *mcp.CallToolResult {
	return utils.ToolErrorResult("dashboard is locked " + lockAuthor(lock) + ", so nothing was written. " + lockRecovery)
}

func lockAuthor(lock dashboardLock) string {
	createdBy := lock.CreatedBy
	if createdBy == "" {
		createdBy = "unknown"
	}
	return "(created_by: " + createdBy + ")"
}

const lockRecovery = "Ask its author or an org admin to unlock it with set_dashboard_lock, " +
	"or copy it with create_dashboard and edit the copy."

// forbiddenWriteResult serves any other write 403 (a built-in dashboard) as a user error.
func forbiddenWriteResult(err error) *mcp.CallToolResult {
	return utils.ToolErrorResult(fmt.Sprintf(
		"dashboard is readonly and cannot be modified, so nothing was written; copy it with create_dashboard and edit the copy: %v", err))
}
