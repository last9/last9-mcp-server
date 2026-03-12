package logs

import (
	"context"
	"log"

	last9mcp "github.com/last9/mcp-go-sdk/mcp"
)

func logToolf(ctx context.Context, toolName, format string, args ...any) {
	clientName := "unknown_client"
	if clientInfo, ok := ctx.Value("clientInfo").(last9mcp.ClientInfo); ok && clientInfo.Name != "" {
		clientName = clientInfo.Name
	}

	logArgs := append([]any{clientName, toolName}, args...)
	log.Printf("🧩 [%s] %s "+format, logArgs...)
}

func logGetLogsf(ctx context.Context, format string, args ...any) {
	logToolf(ctx, "get_logs", format, args...)
}

func logServiceLogsf(ctx context.Context, format string, args ...any) {
	logToolf(ctx, "get_service_logs", format, args...)
}
