# Last9 MCP Server - Cloudflare Workers

This is the Cloudflare Workers version of the Last9 MCP Server, enabling remote hosting and access to Last9's observability data through the Model Context Protocol.

## Features

- **Remote MCP Server**: Deployed on Cloudflare Workers for internet-accessible MCP tools
- **Full Last9 Integration**: Access to logs, metrics, traces, alerts, and more
- **OAuth Authentication**: Secure access control for remote connections
- **TypeScript**: Full type safety and modern JavaScript features
- **Scalable**: Leverages Cloudflare's global network

## Available Tools

This server implements all the same tools as the Go version:

### Observability & APM Tools
- `get_exceptions`: Get the list of exceptions
- `get_service_summary`: Get service summary with throughput, error rate, and response time
- `get_service_environments`: Get available environments for services
- `get_service_performance_details`: Get detailed performance metrics for a service
- `get_service_operations_summary`: Get operations summary for a service
- `get_service_dependency_graph`: Get service dependency graph

### Prometheus/PromQL Tools
- `prometheus_range_query`: Execute PromQL range queries for metrics data
- `prometheus_instant_query`: Execute PromQL instant queries for metrics data
- `prometheus_label_values`: Get label values for PromQL queries
- `prometheus_labels`: Get available labels for PromQL queries

### Logs Management
- `get_logs`: Get logs filtered by service name and/or severity level
- `get_service_logs`: Get raw log entries for a specific service over a time range
- `get_drop_rules`: Get drop rules for logs
- `add_drop_rule`: Create a drop rule for logs

### Traces Management
- `get_service_traces`: Query traces for a specific service with filtering options

### Alert Management
- `get_alert_config`: Get alert configurations from Last9
- `get_alerts`: Get currently active alerts

## Development

### Prerequisites

- Node.js 18+
- Wrangler CLI
- Last9 account with API access

### Setup

1. **Install dependencies:**
   ```bash
   npm install
   ```

2. **Configure environment variables:**
   Copy `.dev.vars` and set your Last9 credentials:
   ```bash
   cp .dev.vars .dev.vars.local
   # Edit .dev.vars.local with your actual values
   ```

3. **Start development server:**
   ```bash
   npm run dev
   ```

### Environment Variables

Required environment variables:

- `LAST9_BASE_URL`: Last9 API URL from OTel integration
- `LAST9_AUTH_TOKEN`: Authentication token for Last9 MCP server
- `LAST9_REFRESH_TOKEN`: Refresh token with write permissions

Optional (for OAuth):
- `OAUTH_CLIENT_ID`: OAuth client ID for authentication
- `OAUTH_CLIENT_SECRET`: OAuth client secret

### Testing Locally

You can test the MCP server using curl:

```bash
# List available tools
curl -X POST http://localhost:8787/mcp \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: session_$(date +%s)000000000" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list",
    "params": {}
  }'

# Get service logs
curl -X POST http://localhost:8787/mcp \
  -H "Content-Type: application/json" \
  -H "Mcp-Session-Id: session_$(date +%s)000000000" \
  -d '{
    "jsonrpc": "2.0",
    "id": 2,
    "method": "tools/call",
    "params": {
      "name": "get_service_logs",
      "arguments": {
        "service_name": "your-service-name",
        "lookback_minutes": 30,
        "limit": 10
      }
    }
  }'
```

## Deployment

### Deploy to Cloudflare Workers

1. **Login to Wrangler:**
   ```bash
   wrangler login
   ```

2. **Set production secrets:**
   ```bash
   wrangler secret put LAST9_BASE_URL
   wrangler secret put LAST9_AUTH_TOKEN
   wrangler secret put LAST9_REFRESH_TOKEN
   ```

3. **Deploy:**
   ```bash
   npm run deploy
   ```

### Using with MCP Clients

After deployment, you can connect to your remote MCP server from clients like Claude Desktop:

```json
{
  "mcpServers": {
    "last9-remote": {
      "command": "npx",
      "args": ["@anthropic-ai/mcp-client", "https://your-worker.your-subdomain.workers.dev/mcp"],
      "env": {}
    }
  }
}
```

## Architecture

The server is structured as follows:

- `src/index.ts`: Main entry point and request handler
- `src/types.ts`: TypeScript type definitions
- `src/utils.ts`: Utility functions for authentication and API calls
- `src/tools/`: Individual tool implementations organized by category
  - `apm.ts`: APM and service monitoring tools
  - `logs.ts`: Log management tools
  - `traces.ts`: Tracing and exception tools
  - `alerting.ts`: Alert management tools
  - `prometheus.ts`: Prometheus/PromQL tools

## Security

- All API calls use Bearer token authentication
- Tokens are automatically refreshed using refresh tokens
- CORS is configured for web client access
- Environment variables are secured through Wrangler secrets

## Limitations

- Cloudflare Workers have a 10ms CPU time limit per request
- Large result sets may need pagination (handled automatically)
- Cold starts may add latency to first requests

## Contributing

This is part of the larger Last9 MCP Server project. Please see the main repository for contribution guidelines.

## License

Apache-2.0