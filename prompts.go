package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"last9-mcp/internal/knowledge"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed prompts/workflows/*.md
var workflowFS embed.FS

//go:embed prompts/references/*.md
var referenceFS embed.FS

// promptDef defines a prompt's metadata and which reference files it needs.
type promptDef struct {
	prompt    *mcp.Prompt
	workflow  string   // filename under prompts/workflows/
	refs      []string // filenames under prompts/references/
	argNames  []string // argument names used as $UPPER_SNAKE placeholders in workflow text
}

var promptDefs = []promptDef{
	{
		prompt: &mcp.Prompt{
			Name:        "k8s-infra-analysis",
			Title:       "K8s Infrastructure Analysis",
			Description: "Investigate Kubernetes infrastructure issues: pod crashes, OOM kills, node pressure, HPA issues, resource limit tuning, disk IOPS problems, network bottlenecks, scheduling failures, evictions, or deployment rollout issues.",
			Arguments: []*mcp.PromptArgument{
				{Name: "service_name", Description: "Name of the affected service", Required: false},
				{Name: "namespace", Description: "Kubernetes namespace", Required: false},
				{Name: "environment", Description: "Environment (e.g., production, staging)", Required: false},
			},
		},
		workflow: "k8s-infra-analysis.md",
		refs:     []string{"investigation-framework.md", "prometheus-k8s-queries.md"},
		argNames: []string{"service_name", "namespace", "environment"},
	},
	{
		prompt: &mcp.Prompt{
			Name:        "app-performance-analysis",
			Title:       "App Performance Analysis",
			Description: "Investigate application-layer performance issues: slow endpoints, high latency, error rate spikes, DB query performance, connection pool exhaustion, timeout errors, inter-service call failures, Kafka/messaging consumer lag, circuit breaker trips, or retry storms.",
			Arguments: []*mcp.PromptArgument{
				{Name: "service_name", Description: "Name of the affected service", Required: false},
				{Name: "environment", Description: "Environment (e.g., production, staging)", Required: false},
				{Name: "start_time", Description: "Investigation window start (ISO 8601 or Unix epoch)", Required: false},
				{Name: "end_time", Description: "Investigation window end (ISO 8601 or Unix epoch)", Required: false},
			},
		},
		workflow: "app-performance-analysis.md",
		refs:     []string{"investigation-framework.md", "apm-tool-patterns.md"},
		argNames: []string{"service_name", "environment", "start_time", "end_time"},
	},
	{
		prompt: &mcp.Prompt{
			Name:        "incident-rca",
			Title:       "Incident Root Cause Analysis",
			Description: "Perform root cause analysis for an incident: availability drops, latency spikes, error rate increases, apdex drops, SLO/SLA breaches, outage diagnosis, postmortem data gathering, or alert investigation.",
			Arguments: []*mcp.PromptArgument{
				{Name: "service_name", Description: "Name of the affected service", Required: false},
				{Name: "environment", Description: "Environment (e.g., production, staging)", Required: false},
				{Name: "start_time", Description: "Incident start time (ISO 8601 or Unix epoch)", Required: false},
				{Name: "end_time", Description: "Incident end time (ISO 8601 or Unix epoch)", Required: false},
			},
		},
		workflow: "incident-rca.md",
		refs:     []string{"investigation-framework.md", "apm-tool-patterns.md", "rca-note-template.md"},
		argNames: []string{"service_name", "environment", "start_time", "end_time"},
	},
}

// registerAllPrompts registers all SRE workflow prompts with the MCP server.
func registerAllPrompts(server *last9mcp.Last9MCPServer, kStore knowledge.Store) {
	for _, def := range promptDefs {
		handler := makePromptHandler(def, kStore)
		server.Server.AddPrompt(def.prompt, handler)
	}
}

// makePromptHandler returns a PromptHandler closure for the given prompt definition.
// It reads embedded workflow and reference files, substitutes argument placeholders,
// and optionally pre-loads knowledge graph context when service_name is provided.
func makePromptHandler(def promptDef, kStore knowledge.Store) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		args := req.Params.Arguments

		// Read workflow content
		workflowContent, err := workflowFS.ReadFile("prompts/workflows/" + def.workflow)
		if err != nil {
			return nil, fmt.Errorf("failed to read workflow %s: %w", def.workflow, err)
		}
		workflow := string(workflowContent)

		// Substitute argument placeholders ($SERVICE_NAME, $NAMESPACE, etc.)
		workflow = substituteArgs(workflow, args, def.argNames)

		// Build messages
		var messages []*mcp.PromptMessage

		// If service_name is provided, pre-load knowledge graph context
		if serviceName, ok := args["service_name"]; ok && serviceName != "" && kStore != nil {
			kgContext := buildKGContext(ctx, kStore, serviceName)
			if kgContext != "" {
				messages = append(messages, &mcp.PromptMessage{
					Role:    mcp.Role("assistant"),
					Content: &mcp.TextContent{Text: kgContext},
				})
			}
		}

		// Read and concatenate reference files
		var refParts []string
		for _, refFile := range def.refs {
			content, err := referenceFS.ReadFile("prompts/references/" + refFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read reference %s: %w", refFile, err)
			}
			refParts = append(refParts, string(content))
		}
		if len(refParts) > 0 {
			refContent := strings.Join(refParts, "\n\n---\n\n")
			messages = append(messages, &mcp.PromptMessage{
				Role: mcp.Role("assistant"),
				Content: &mcp.TextContent{
					Text: "# Reference Material\n\nThe following reference material is pre-loaded for this investigation. Use it as needed during the workflow.\n\n" + refContent,
				},
			})
		}

		// Workflow as user message
		messages = append(messages, &mcp.PromptMessage{
			Role:    mcp.Role("user"),
			Content: &mcp.TextContent{Text: workflow},
		})

		return &mcp.GetPromptResult{
			Description: def.prompt.Description,
			Messages:    messages,
		}, nil
	}
}

// substituteArgs replaces $UPPER_SNAKE placeholders in text with argument values.
// For example, "service_name" → replaces "$SERVICE_NAME" with the provided value.
// If an argument is not provided, the placeholder is left as-is (the agent fills it in).
func substituteArgs(text string, args map[string]string, argNames []string) string {
	for _, name := range argNames {
		val, ok := args[name]
		if !ok || val == "" {
			continue
		}
		placeholder := "$" + strings.ToUpper(strings.ReplaceAll(name, " ", "_"))
		text = strings.ReplaceAll(text, placeholder, val)
	}
	return text
}

// buildKGContext queries the knowledge graph for prior findings about a service
// and formats them as pre-loaded context for the investigation.
func buildKGContext(ctx context.Context, kStore knowledge.Store, serviceName string) string {
	result, err := kStore.Search(ctx, serviceName, 10)
	if err != nil || result == nil {
		return ""
	}

	// Only produce context if there are actual findings
	if len(result.Nodes) == 0 && len(result.Notes) == 0 && len(result.Edges) == 0 {
		return ""
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return ""
	}

	return fmt.Sprintf(
		"# Prior Knowledge Graph Context for %q\n\n"+
			"The following prior findings were found in the knowledge graph. "+
			"Review these before starting the investigation — they may reveal "+
			"known issues, previous RCAs, ownership, or topology.\n\n```json\n%s\n```",
		serviceName, string(data),
	)
}
