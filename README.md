# Last9 MCP Server

A [Model Context Protocol](https://modelcontextprotocol.io/) server implementation for [Last9](https://last9.io) that enables AI agents to query your data using Last9.

## Status

Works with Claude desktop app. Implements two MCP [tools](https://modelcontextprotocol.io/docs/concepts/tools):

- get_exceptions: Get list of execeptions
- get_servicegraph: Get Service graph for an endpoint from the exception


## Installation

### Releases

```
# Add the Last9 tap
brew tap last9/tap

# Install the Last9 MCP CLI
brew install last9-mcp
```

## Configuration

### Environment Variables

The service requires the following environment variables:

- `LAST9_AUTH_TOKEN`: Authentication token for Last9 MCP server (required)
- `LAST9_BASE_URL`: Last9 API URL (required)

## Usage with Claude Desktop

Configure the Claude app to use the MCP server:

```bash
code ~/Library/Application\ Support/Claude/claude_desktop_config.json
```

```json
{
  "mcpServers": {
    "last9": {
      "command": "/opt/homebrew/bin/last9-mcp",
      "env": {
        "LAST9_AUTH_TOKEN": "your_auth_token",
        "LAST9_BASE_URL": "https://otlp.last9.io"
      }
    }
  }
}
```