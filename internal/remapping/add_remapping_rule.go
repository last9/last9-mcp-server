package remapping

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	apiremapping "last9-mcp/internal/last9api/remapping"
	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RemappingPrecondition scopes logs_extract rules to matching lines.
type RemappingPrecondition struct {
	Key      string `json:"key" jsonschema:"(Required) Attribute key to match (e.g. attributes[\"severity\"])"`
	Value    string `json:"value" jsonschema:"(Required) Value to compare against"`
	Operator string `json:"operator" jsonschema:"Comparison operator: equals, not_equals, or like (default: equals)"`
}

// AddRemappingRuleArgs creates a remapping rule.
type AddRemappingRuleArgs struct {
	RuleType        string                  `json:"rule_type" jsonschema:"(Required) Remapping rule type: logs_extract, logs_map, or traces_map"`
	Name            string                  `json:"name" jsonschema:"(Required) Descriptive name for the rule"`
	RemapKeys       []string                `json:"remap_keys" jsonschema:"(Required) Source field(s) to remap from, evaluated left to right for map rules"`
	TargetAttribute string                  `json:"target_attribute" jsonschema:"(Required) Target attribute. logs_extract: log_attributes or resource_attributes. logs_map: service, severity, or resource_deployment.environment. traces_map: service"`
	Region          string                  `json:"region,omitempty" jsonschema:"AWS region (defaults to configured datasource region)"`
	ExtractType     string                  `json:"extract_type,omitempty" jsonschema:"logs_extract only. Extraction method: json or pattern (regex with named capture groups)"`
	Action          string                  `json:"action,omitempty" jsonschema:"logs_extract only. insert or upsert (default: upsert)"`
	Prefix          string                  `json:"prefix,omitempty" jsonschema:"logs_extract only. Optional prefix added to extracted field names"`
	Preconditions   []RemappingPrecondition `json:"preconditions,omitempty" jsonschema:"logs_extract only. At most one scope filter; apply extraction only to matching lines"`
}

func validateRemapKeys(remapKeys []string) error {
	for i, key := range remapKeys {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("remap_keys[%d] must not be empty", i)
		}
	}
	return nil
}

func validatePatternRemapKey(pattern string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern regex: %w", err)
	}
	for _, name := range re.SubexpNames() {
		if name != "" {
			return nil
		}
	}
	return errors.New("pattern must include at least one named capture group (e.g. (?P<severity>DEBUG|INFO))")
}

func validateLikePreconditionValue(value string) error {
	if _, err := regexp.Compile(value); err != nil {
		return fmt.Errorf("invalid like precondition value regex: %w", err)
	}
	return nil
}

func validateAddRemappingRuleArgs(args AddRemappingRuleArgs) error {
	if args.RuleType == "" {
		return errors.New("rule_type is required")
	}
	if err := validateRuleType(args.RuleType); err != nil {
		return err
	}
	if args.Name == "" {
		return errors.New("name is required")
	}
	if len(args.RemapKeys) == 0 {
		return errors.New("remap_keys must be provided")
	}
	if err := validateRemapKeys(args.RemapKeys); err != nil {
		return err
	}
	if args.TargetAttribute == "" {
		return errors.New("target_attribute is required")
	}

	switch args.RuleType {
	case ruleTypeLogsExtract:
		if args.ExtractType == "" {
			return errors.New("extract_type is required for logs_extract rules")
		}
		if args.ExtractType != "json" && args.ExtractType != "pattern" {
			return fmt.Errorf("invalid extract_type %q: must be json or pattern", args.ExtractType)
		}
		if args.ExtractType == "pattern" {
			if len(args.RemapKeys) != 1 {
				return errors.New("pattern extraction requires exactly one remap_keys entry")
			}
			if err := validatePatternRemapKey(args.RemapKeys[0]); err != nil {
				return err
			}
		}
		if args.TargetAttribute != "log_attributes" && args.TargetAttribute != "resource_attributes" {
			return errors.New("target_attribute must be log_attributes or resource_attributes for logs_extract rules")
		}
		action := args.Action
		if action == "" {
			action = "upsert"
		}
		if action != "insert" && action != "upsert" {
			return fmt.Errorf("invalid action %q: must be insert or upsert", action)
		}
		if len(args.Preconditions) > 1 {
			return errors.New("only one precondition is supported for logs_extract rules")
		}
		for _, p := range args.Preconditions {
			if p.Key == "" {
				return errors.New("precondition key is required")
			}
			if p.Value == "" {
				return errors.New("precondition value is required")
			}
			operator := p.Operator
			if operator == "" {
				operator = "equals"
			}
			if operator != "equals" && operator != "not_equals" && operator != "like" {
				return fmt.Errorf("invalid precondition operator %q: must be equals, not_equals, or like", operator)
			}
			if operator == "like" {
				if err := validateLikePreconditionValue(p.Value); err != nil {
					return err
				}
			}
		}
	case ruleTypeLogsMap:
		if args.ExtractType != "" || args.Action != "" || args.Prefix != "" || len(args.Preconditions) > 0 {
			return errors.New("extract_type, action, prefix, and preconditions are only valid for logs_extract rules")
		}
		switch args.TargetAttribute {
		case "service", "severity", "resource_deployment.environment":
		default:
			return errors.New("target_attribute must be service, severity, or resource_deployment.environment for logs_map rules")
		}
	case ruleTypeTracesMap:
		if args.ExtractType != "" || args.Action != "" || args.Prefix != "" || len(args.Preconditions) > 0 {
			return errors.New("extract_type, action, prefix, and preconditions are only valid for logs_extract rules")
		}
		if args.TargetAttribute != "service" {
			return errors.New("target_attribute must be service for traces_map rules")
		}
	}

	return nil
}

func buildCreateRemappingRuleInput(args AddRemappingRuleArgs) apiremapping.CreateRemappingRuleInput {
	in := apiremapping.CreateRemappingRuleInput{Name: args.Name}
	if args.RuleType != ruleTypeLogsExtract {
		in.Properties = apiremapping.MapProperties{
			RemapKeys:        args.RemapKeys,
			TargetAttributes: args.TargetAttribute,
		}
		return in
	}

	action := args.Action
	if action == "" {
		action = "upsert"
	}
	props := apiremapping.LogsExtractProperties{
		Type:             args.ExtractType,
		RemapKeys:        args.RemapKeys,
		TargetAttributes: args.TargetAttribute,
		Action:           action,
		Prefix:           args.Prefix,
	}
	for _, p := range args.Preconditions {
		operator := p.Operator
		if operator == "" {
			operator = "equals"
		}
		props.Preconditions = append(props.Preconditions, apiremapping.Precondition{
			Key:      p.Key,
			Value:    p.Value,
			Operator: operator,
		})
	}
	in.Properties = props
	return in
}

// NewAddRemappingRuleHandler creates a remapping rule.
func NewAddRemappingRuleHandler(client *http.Client, cfg models.Config) func(context.Context, *mcp.CallToolRequest, AddRemappingRuleArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args AddRemappingRuleArgs) (*mcp.CallToolResult, any, error) {
		if err := validateAddRemappingRuleArgs(args); err != nil {
			return nil, nil, err
		}

		api, err := newRegionClient(client, cfg, args.Region)
		if err != nil {
			return nil, nil, err
		}

		raw, err := apiremapping.CreateRemappingRule(ctx, api, args.RuleType, buildCreateRemappingRuleInput(args))
		if err != nil {
			return nil, nil, err
		}

		result, err := remappingResult(cfg, raw)
		if err != nil {
			return nil, nil, err
		}
		return result, nil, nil
	}
}
