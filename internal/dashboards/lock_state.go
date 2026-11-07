package dashboards

import (
	"bytes"
	"encoding/json"
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
