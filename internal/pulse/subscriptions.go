package pulse

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"last9-mcp/internal/models"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListSubscriptionsArgs struct{}

type GetSubscriptionArgs struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"(Required) Subscription ID returned by list_pulse_subscriptions."`
}

type SubscriptionInput struct {
	Name          string         `json:"name" jsonschema:"(Required) Human-readable subscription name."`
	Schedule      string         `json:"schedule" jsonschema:"(Required) Explicit cron schedule; no cadence is inferred."`
	Timezone      string         `json:"timezone" jsonschema:"(Required) IANA timezone for the schedule."`
	Recipients    []string       `json:"recipients" jsonschema:"(Required) Complete email recipient list."`
	Scope         map[string]any `json:"scope" jsonschema:"(Required) Explicit alert scope object."`
	Config        map[string]any `json:"config" jsonschema:"(Required) Measured analysis configuration; no defaults are invented."`
	ConfigVersion string         `json:"config_version" jsonschema:"(Required) Configuration contract version."`
}

type CreateSubscriptionArgs struct {
	SubscriptionInput
	Confirmed bool `json:"confirmed" jsonschema:"(Required) Must be true after the user confirms creation. Creation remains disabled."`
}

type UpdateSubscriptionArgs struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"(Required) Subscription ID to replace."`
	SubscriptionInput
	Confirmed bool `json:"confirmed" jsonschema:"(Required) Must be true after the user confirms the full replacement."`
}

type SetSubscriptionEnabledArgs struct {
	SubscriptionID string `json:"subscription_id" jsonschema:"(Required) Subscription ID to change."`
	Confirmed      bool   `json:"confirmed" jsonschema:"(Required) Must be true after the user confirms the enablement change."`
}

func NewListSubscriptionsHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, ListSubscriptionsArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ ListSubscriptionsArgs) (*mcp.CallToolResult, any, error) {
		body, err := api.call(ctx, request{method: http.MethodGet, path: "/subscriptions"})
		return handlerResult(body, err)
	}
}

func NewGetSubscriptionHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, GetSubscriptionArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args GetSubscriptionArgs) (*mcp.CallToolResult, any, error) {
		id, err := escapedID(args.SubscriptionID)
		if err != nil {
			return nil, nil, err
		}
		body, err := api.call(ctx, request{method: http.MethodGet, path: "/subscriptions/" + id})
		return handlerResult(body, err)
	}
}

func NewCreateSubscriptionHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, CreateSubscriptionArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args CreateSubscriptionArgs) (*mcp.CallToolResult, any, error) {
		if err := validateSubscriptionMutation(args.SubscriptionInput, args.Confirmed); err != nil {
			return nil, nil, err
		}
		body, err := api.call(ctx, request{method: http.MethodPost, path: "/subscriptions", body: args.SubscriptionInput})
		return handlerResult(body, err)
	}
}

func NewUpdateSubscriptionHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, UpdateSubscriptionArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args UpdateSubscriptionArgs) (*mcp.CallToolResult, any, error) {
		id, err := validateUpdate(args)
		if err != nil {
			return nil, nil, err
		}
		body, err := api.call(ctx, request{method: http.MethodPut, path: "/subscriptions/" + id, body: args.SubscriptionInput})
		return handlerResult(body, err)
	}
}

func NewEnableSubscriptionHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, SetSubscriptionEnabledArgs) (*mcp.CallToolResult, any, error) {
	return newEnablementHandler(httpClient, config, true)
}

func NewDisableSubscriptionHandler(httpClient *http.Client, config models.Config) func(context.Context, *mcp.CallToolRequest, SetSubscriptionEnabledArgs) (*mcp.CallToolResult, any, error) {
	return newEnablementHandler(httpClient, config, false)
}

func newEnablementHandler(httpClient *http.Client, config models.Config, enabled bool) func(context.Context, *mcp.CallToolRequest, SetSubscriptionEnabledArgs) (*mcp.CallToolResult, any, error) {
	api := newClient(httpClient, config)
	return func(ctx context.Context, _ *mcp.CallToolRequest, args SetSubscriptionEnabledArgs) (*mcp.CallToolResult, any, error) {
		id, err := validateEnablement(args)
		if err != nil {
			return nil, nil, err
		}
		body, err := api.call(ctx, request{method: http.MethodPatch, path: "/subscriptions/" + id + "/enabled", body: map[string]bool{"enabled": enabled}})
		return handlerResult(body, err)
	}
}

func validateSubscriptionMutation(input SubscriptionInput, confirmed bool) error {
	if !confirmed {
		return fmt.Errorf("confirmed must be true after the user approves this write")
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Schedule) == "" || strings.TrimSpace(input.Timezone) == "" {
		return fmt.Errorf("name, schedule, and timezone are required")
	}
	if len(input.Recipients) == 0 || input.Scope == nil || input.Config == nil || strings.TrimSpace(input.ConfigVersion) == "" {
		return fmt.Errorf("recipients, scope, config, and config_version are required")
	}
	return nil
}

func validateUpdate(args UpdateSubscriptionArgs) (string, error) {
	if err := validateSubscriptionMutation(args.SubscriptionInput, args.Confirmed); err != nil {
		return "", err
	}
	return escapedID(args.SubscriptionID)
}

func validateEnablement(args SetSubscriptionEnabledArgs) (string, error) {
	if !args.Confirmed {
		return "", fmt.Errorf("confirmed must be true after the user approves this write")
	}
	return escapedID(args.SubscriptionID)
}

func handlerResult(body []byte, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	return textResult(body), nil, nil
}
