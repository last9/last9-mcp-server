package main

import (
	"context"
	"testing"
	"time"

	"last9-mcp/internal/attributes"
	"last9-mcp/internal/auth"
	"last9-mcp/internal/models"
	"last9-mcp/internal/toolsets"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReferenceResourcesAvailableUnderMetricsToolset(t *testing.T) {
	allowed, err := toolsets.Parse("metrics")
	if err != nil {
		t.Fatal(err)
	}

	server, err := last9mcp.NewServerWithOptions("last9-mcp", Version, last9mcp.WithSkipProviderInit())
	if err != nil {
		t.Fatal(err)
	}
	registerReferenceResources(server)

	cfg := models.Config{TokenManager: &auth.TokenManager{}, AllowedTools: allowed}
	attrCache := attributes.NewAttributeCache(auth.GetHTTPClient(), cfg)
	if err := registerAllTools(server, cfg, attrCache); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "resources-test", Version: Version}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	found := map[string]bool{}
	for res, err := range session.Resources(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		found[res.URI] = true
	}
	for _, uri := range []string{resourceURILogjson, resourceURITracejson, resourceURIServiceLogs, resourceURIMetrics} {
		if !found[uri] {
			t.Errorf("resource %q missing under metrics toolset", uri)
		}
	}

	read, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: resourceURILogjson})
	if err != nil {
		t.Fatalf("resources/read logjson: %v", err)
	}
	if len(read.Contents) == 0 || len(read.Contents[0].Text) < 1000 {
		t.Fatalf("logjson resource body too short: %#v", read.Contents)
	}

	metricsRead, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: resourceURIMetrics})
	if err != nil {
		t.Fatalf("resources/read metrics: %v", err)
	}
	if len(metricsRead.Contents) == 0 || len(metricsRead.Contents[0].Text) < 500 {
		t.Fatalf("metrics resource body too short: %#v", metricsRead.Contents)
	}
}
