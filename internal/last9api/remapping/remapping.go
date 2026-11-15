package remapping

import (
	"context"
	"encoding/json"
	"net/http"

	"last9-mcp/internal/last9api"
)

const pathRemapping = "/otel_settings/remapping/"

const (
	RuleTypeLogsExtract = "logs_extract"
	RuleTypeLogsMap     = "logs_map"
	RuleTypeTracesMap   = "traces_map"
)

// Precondition scopes a logs_extract rule to matching lines.
type Precondition struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Operator string `json:"operator"`
}

type LogsExtractProperties struct {
	Type             string         `json:"type"`
	Preconditions    []Precondition `json:"preconditions,omitempty"`
	RemapKeys        []string       `json:"remap_keys"`
	TargetAttributes string         `json:"target_attribute"`
	Action           string         `json:"action"`
	Prefix           string         `json:"prefix,omitempty"`
}

// MapProperties is the properties block of logs_map and traces_map rules.
type MapProperties struct {
	RemapKeys        []string `json:"remap_keys"`
	TargetAttributes string   `json:"target_attribute"`
}

// CreateRemappingRuleInput carries LogsExtractProperties or MapProperties, matching the
// rule type it is posted to.
type CreateRemappingRuleInput struct {
	Name       string `json:"name"`
	Properties any    `json:"properties"`
}

func rulesPath(ruleType string) string { return pathRemapping + ruleType }

// ListRemappingRules calls GET /otel_settings/remapping/{rule_type}.
func ListRemappingRules(ctx context.Context, c *last9api.Client, ruleType string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.Do(ctx, "list remapping rules",
		last9api.Request{Method: http.MethodGet, Path: rulesPath(ruleType)}, &out, last9api.WithRegionQuery())
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreateRemappingRule calls POST /otel_settings/remapping/{rule_type}.
func CreateRemappingRule(ctx context.Context, c *last9api.Client, ruleType string, in CreateRemappingRuleInput) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.Do(ctx, "create remapping rule",
		last9api.Request{Method: http.MethodPost, Path: rulesPath(ruleType), Body: in}, &out, last9api.WithRegionQuery())
	if err != nil {
		return nil, err
	}
	return out, nil
}
