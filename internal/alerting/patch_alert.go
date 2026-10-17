package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/last9api"
	"last9-mcp/internal/last9api/entities"
	"last9-mcp/internal/models"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PatchAlertArgs struct {
	EntityID   string           `json:"entity_id" jsonschema:"(Required) Alert group/entity UUID that owns the rule"`
	ID         string           `json:"id" jsonschema:"(Required) Alert rule UUID"`
	IsDisabled *bool            `json:"is_disabled,omitempty" jsonschema:"Set false to promote/enable or true to pause/disable"`
	MuteUntil  *int64           `json:"mute_until,omitempty" jsonschema:"Unix epoch mute deadline; 0 unmutes when the entity itself is not snoozed"`
	Properties *AlertProperties `json:"properties,omitempty" jsonschema:"Change responder description, runbook, annotations, or query mode without recreating the rule. Each field sent replaces the stored one (annotations as a whole map; {} clears them); omitted or blank fields keep their stored value"`

	RecurringMuteSchedule      *RecurringMuteSchedule `json:"recurring_mute_schedule,omitempty" jsonschema:"Replace the weekly recurring mute schedule (timezone plus up to 20 windows)"`
	ClearRecurringMuteSchedule bool                   `json:"clear_recurring_mute_schedule,omitempty" jsonschema:"Set true to remove the rule's recurring mute schedule entirely (default false); cannot be combined with recurring_mute_schedule"`
}

type alertPatchBody struct {
	IsDisabled *bool            `json:"is_disabled,omitempty"`
	MuteUntil  *int64           `json:"mute_until,omitempty"`
	Properties *AlertProperties `json:"properties,omitempty"`

	RecurringMuteSchedule json.RawMessage `json:"recurring_mute_schedule,omitempty"`
}

func NewPatchAlertHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, PatchAlertArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args PatchAlertArgs) (*mcp.CallToolResult, any, error) {
		if strings.TrimSpace(args.EntityID) == "" {
			return utils.ToolErrorResult("entity_id is required"), nil, nil
		}
		if err := validateAlertRuleID(args.ID); err != nil {
			return utils.ToolErrorResult(err.Error()), nil, nil
		}
		if args.IsDisabled == nil && args.MuteUntil == nil && args.Properties == nil && args.RecurringMuteSchedule == nil && !args.ClearRecurringMuteSchedule {
			return utils.ToolErrorResult("at least one of is_disabled, mute_until, properties, recurring_mute_schedule, or clear_recurring_mute_schedule is required"), nil, nil
		}
		if args.ClearRecurringMuteSchedule && args.RecurringMuteSchedule != nil {
			return utils.ToolErrorResult("clear_recurring_mute_schedule cannot be combined with recurring_mute_schedule"), nil, nil
		}
		if args.RecurringMuteSchedule != nil {
			if err := validateRecurringMuteSchedule(args.RecurringMuteSchedule); err != nil {
				return utils.ToolErrorResult(err.Error()), nil, nil
			}
		}
		body := alertPatchBody{IsDisabled: args.IsDisabled, MuteUntil: args.MuteUntil}
		if args.ClearRecurringMuteSchedule {
			body.RecurringMuteSchedule = json.RawMessage("null")
		} else if args.RecurringMuteSchedule != nil {
			raw, err := json.Marshal(args.RecurringMuteSchedule)
			if err != nil {
				return nil, nil, err
			}
			body.RecurringMuteSchedule = raw
		}
		api := last9api.NewClient(client, cfg)
		rule, refusal, err := requireRuleInGroup(ctx, api, args.EntityID, args.ID)
		if refusal != nil || err != nil {
			return refusal, nil, err
		}
		if args.Properties != nil {
			merged, err := mergeAlertProperties(rule.Properties, *args.Properties)
			if err != nil {
				return utils.ToolErrorResult(fmt.Sprintf("could not read the stored properties of alert rule %s (%v); nothing was changed", args.ID, err)), nil, nil
			}
			body.Properties = &merged
		}
		err = entities.PatchAlertRule(ctx, api, entities.AlertRuleInput{
			EntityID: args.EntityID,
			RuleID:   args.ID,
			Body:     body,
		})
		if err != nil {
			var apiErr *utils.UpstreamHTTPError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
				return utils.ToolErrorResult("patch_alert was forbidden (HTTP 403). Viewer-role users may only change the mute deadline (mute_until, on its own); pausing, properties, recurring_mute_schedule, clear_recurring_mute_schedule and every other change need editor access."), nil, nil
			}
			return nil, nil, withUpstreamBody(err)
		}
		return alertMutationResult(cfg, nil, fmt.Sprintf(`{"patched":true,"id":%q}`, args.ID)), nil, nil
	}
}

// The API overwrites the whole stored properties object on PATCH, so fields the caller
// left out or blank are carried over from the stored rule instead of being cleared.
func mergeAlertProperties(stored json.RawMessage, change AlertProperties) (AlertProperties, error) {
	var merged AlertProperties
	if len(stored) > 0 {
		if err := json.Unmarshal(stored, &merged); err != nil {
			return merged, err
		}
	}
	if strings.TrimSpace(change.Description) != "" {
		merged.Description = change.Description
	}
	if change.Runbook != nil {
		merged.Runbook = change.Runbook
	}
	if change.Annotations != nil {
		merged.Annotations = change.Annotations
	}
	if strings.TrimSpace(change.QueryMode) != "" {
		merged.QueryMode = change.QueryMode
	}
	return merged, nil
}
