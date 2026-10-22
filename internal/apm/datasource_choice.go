package apm

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"last9-mcp/internal/last9api/prom"
	"last9-mcp/internal/models"
	"last9-mcp/internal/namematch"
	"last9-mcp/internal/utils"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type datasourceChoice struct {
	cfg        models.Config
	note       string
	candidates []string
}

func chooseDatasource(cfg models.Config, requested string) (datasourceChoice, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		var note string
		if name := defaultDatasourceName(cfg); name != "" {
			note = fmt.Sprintf("Using datasource %q (default).", name)
		}
		return datasourceChoice{cfg: cfg, note: note}, nil
	}

	names := make([]string, 0, len(cfg.Datasources))
	for _, ds := range cfg.Datasources {
		names = append(names, ds.Name)
	}
	match := namematch.Resolve(requested, names)
	switch match.Kind {
	case namematch.Exact, namematch.Closest:
		resolved, err := resolveDatasourceCfg(cfg, match.Name)
		if err != nil {
			return datasourceChoice{}, err
		}
		choice := datasourceChoice{cfg: resolved}
		if match.Kind == namematch.Closest {
			choice.note = fmt.Sprintf("Using datasource %q (closest match to %q).", match.Name, requested)
		}
		return choice, nil
	case namematch.Ambiguous:
		return datasourceChoice{candidates: match.Candidates}, nil
	default:
		sort.Strings(names)
		return datasourceChoice{}, fmt.Errorf("datasource %q not found; available: %s", requested, strings.Join(names, ", "))
	}
}

func defaultDatasourceName(cfg models.Config) string {
	for _, ds := range cfg.Datasources {
		if ds.IsDefault {
			return ds.Name
		}
	}
	return cfg.DatasourceName
}

// ambiguousDatasourceResult is not an error: the agent recovers by picking a
// candidate, and no query ran.
func ambiguousDatasourceResult(requested string, candidates []string) *mcp.CallToolResult {
	text := fmt.Sprintf("datasource %q matches several datasources: %s. No query was run; retry with one of them as datasource.",
		strings.TrimSpace(requested), strings.Join(candidates, ", "))
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func promDatasource(cfg models.Config) prom.Datasource {
	return prom.Datasource{ReadURL: cfg.PrometheusReadURL, Username: cfg.PrometheusUsername, Password: cfg.PrometheusPassword}
}

// An upstream HTTP status is a tool error the agent can act on; anything else
// (transport, config) stays a Go error, as before the move to last9api.
func promToolResult(raw json.RawMessage, err error, note string) (*mcp.CallToolResult, any, error) {
	var upstream *utils.UpstreamHTTPError
	if errors.As(err, &upstream) {
		return utils.ToolErrorResult(err.Error()), nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return promBodyResult(raw, note), nil, nil
}

// promBodyResult serves an upstream body as is, with the datasource note as a
// second block so the body stays parseable.
func promBodyResult(body []byte, note string) *mcp.CallToolResult {
	content := []mcp.Content{&mcp.TextContent{Text: string(body)}}
	if note != "" {
		content = append(content, &mcp.TextContent{Text: note})
	}
	return &mcp.CallToolResult{Content: content}
}
