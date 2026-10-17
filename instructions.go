package main

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"last9-mcp/internal/prompts"
	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverInstructions renders the tool-selection policy, naming only the tools
// that the active toolsets register.
func serverInstructions(allowed toolsets.Set) (string, error) {
	funcs := template.FuncMap{
		"has": allowed.Allows,
		"tools": func(names ...string) string {
			var served []string
			for _, name := range names {
				if allowed.Allows(name) {
					served = append(served, "`"+name+"`")
				}
			}
			return strings.Join(served, ", ")
		},
	}
	tmpl, err := template.New("server_instructions").Funcs(funcs).Parse(prompts.ServerInstructions)
	if err != nil {
		return "", fmt.Errorf("parse server instructions: %w", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, nil); err != nil {
		return "", fmt.Errorf("render server instructions: %w", err)
	}
	return strings.TrimSpace(b.String()), nil
}

// registerServerInstructions sends the policy in the initialize and discover
// results. The SDK wrapper builds the server without ServerOptions, so the
// instructions are set on the way out instead.
func registerServerInstructions(server *last9mcp.Last9MCPServer, allowed toolsets.Set) error {
	instructions, err := serverInstructions(allowed)
	if err != nil {
		return err
	}
	server.Server.AddReceivingMiddleware(instructionsMiddleware(instructions))
	return nil
}

func instructionsMiddleware(instructions string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil {
				return res, err
			}
			switch r := res.(type) {
			case *mcp.InitializeResult:
				r.Instructions = instructions
			case *mcp.DiscoverResult:
				r.Instructions = instructions
			}
			return res, err
		}
	}
}
