# Deployment Guide: Last9 MCP Server on Cloudflare Workers

This guide walks you through deploying the Last9 MCP Server to Cloudflare Workers for remote access.

## Prerequisites

1. **Cloudflare account** with Workers enabled
2. **Last9 account** with API access
3. **Node.js 18+** and npm
4. **Wrangler CLI** installed globally: `npm install -g wrangler`

## Step 1: Get Last9 Credentials

You'll need three pieces of information from your Last9 account:

### 1. LAST9_BASE_URL and LAST9_AUTH_TOKEN

1. Go to [Last9 Integrations](https://app.last9.io/integrations?integration=OpenTelemetry)
2. Click on the OpenTelemetry integration
3. Copy the **OTLP Endpoint URL** → this is your `LAST9_BASE_URL`
4. Copy the **Authentication Token** → this is your `LAST9_AUTH_TOKEN`

### 2. LAST9_REFRESH_TOKEN

1. Go to [Last9 API Access](https://app.last9.io/settings/api-access)
2. Create a new API token with **Write permissions**
3. Copy the token → this is your `LAST9_REFRESH_TOKEN`

## Step 2: Setup Project

1. **Navigate to the Cloudflare directory:**
   ```bash
   cd cloudflare
   ```

2. **Install dependencies:**
   ```bash
   npm install
   ```

3. **Setup local environment (for testing):**
   ```bash
   cp .dev.vars .dev.vars.local
   ```

   Edit `.dev.vars.local` with your actual credentials:
   ```
   LAST9_BASE_URL=https://your-last9-endpoint
   LAST9_AUTH_TOKEN=your_auth_token_here
   LAST9_REFRESH_TOKEN=your_refresh_token_here
   ```

## Step 3: Test Locally

```bash
npm run dev
```

Test with curl:
```bash
curl -X POST http://localhost:8787/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list",
    "params": {}
  }'
```

You should see a list of available MCP tools.

## Step 4: Deploy to Cloudflare

1. **Login to Wrangler:**
   ```bash
   wrangler login
   ```
   This will open a browser to authenticate with Cloudflare.

2. **Set production secrets:**
   ```bash
   wrangler secret put LAST9_BASE_URL
   # Paste your Last9 base URL when prompted

   wrangler secret put LAST9_AUTH_TOKEN
   # Paste your auth token when prompted

   wrangler secret put LAST9_REFRESH_TOKEN
   # Paste your refresh token when prompted
   ```

3. **Deploy the Worker:**
   ```bash
   npm run deploy
   ```

4. **Note your Worker URL:**
   After deployment, Wrangler will show your Worker URL:
   ```
   Published last9-mcp-server (1.23s)
     https://last9-mcp-server.your-subdomain.workers.dev
   ```

## Step 5: Test Remote Deployment

Test your deployed Worker:

```bash
curl -X POST https://last9-mcp-server.your-subdomain.workers.dev/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list",
    "params": {}
  }'
```

## Step 6: Connect from MCP Clients

### Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "last9-remote": {
      "command": "npx",
      "args": ["@modelcontextprotocol/client-remote", "https://your-worker-url.workers.dev/mcp"]
    }
  }
}
```

### Cursor

Add to your Cursor MCP configuration:

```json
{
  "mcpServers": {
    "last9-remote": {
      "command": "npx",
      "args": ["@modelcontextprotocol/client-remote", "https://your-worker-url.workers.dev/mcp"]
    }
  }
}
```

## Troubleshooting

### Common Issues

1. **"Unauthorized" errors:**
   - Check that your `LAST9_AUTH_TOKEN` and `LAST9_REFRESH_TOKEN` are correct
   - Ensure the refresh token has write permissions

2. **"Token expired" errors:**
   - The server should automatically refresh tokens, but you might need to update your refresh token if it's been revoked

3. **CORS errors:**
   - The server includes CORS headers, but some clients might need additional configuration

4. **Rate limiting:**
   - Cloudflare Workers have built-in rate limiting; implement caching if needed

### Debugging

1. **Check Worker logs:**
   ```bash
   wrangler tail
   ```

2. **Test individual tools:**
   ```bash
   curl -X POST https://your-worker-url.workers.dev/mcp \
     -H "Content-Type: application/json" \
     -d '{
       "jsonrpc": "2.0",
       "id": 2,
       "method": "tools/call",
       "params": {
         "name": "get_service_environments",
         "arguments": {}
       }
     }'
   ```

## Updating

To update your deployment:

1. Pull the latest code
2. `npm run deploy`

Secrets (environment variables) persist between deployments.

## Security Considerations

1. **Secrets Management:** Never commit `.dev.vars.local` or expose secrets in code
2. **Access Control:** Consider implementing additional authentication for production use
3. **Rate Limiting:** Monitor usage and implement rate limiting if needed
4. **CORS Policy:** Restrict CORS origins in production if possible

## Custom Domain (Optional)

To use a custom domain:

1. Add a custom domain in Cloudflare Dashboard
2. Update `wrangler.toml`:
   ```toml
   routes = [
     { pattern = "mcp.yourdomain.com/mcp", custom_domain = true }
   ]
   ```
3. Redeploy: `npm run deploy`

## Performance Optimization

- **Caching:** Implement caching for repeated queries
- **Connection Pooling:** Reuse HTTP connections when possible
- **Batch Requests:** Group multiple API calls when feasible
- **Error Handling:** Implement proper error handling and retries