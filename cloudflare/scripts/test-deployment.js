#!/usr/bin/env node

/**
 * Test script to validate MCP server deployment
 */

const https = require('https');
const http = require('http');

const WORKER_URL = process.argv[2] || 'http://localhost:8787';

async function makeRequest(url, data) {
  return new Promise((resolve, reject) => {
    const urlObj = new URL(url);
    const options = {
      hostname: urlObj.hostname,
      port: urlObj.port || (urlObj.protocol === 'https:' ? 443 : 80),
      path: urlObj.pathname,
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Mcp-Session-Id': `session_${Date.now()}000000000`
      }
    };

    const client = urlObj.protocol === 'https:' ? https : http;

    const req = client.request(options, (res) => {
      let responseData = '';

      res.on('data', (chunk) => {
        responseData += chunk;
      });

      res.on('end', () => {
        try {
          const parsed = JSON.parse(responseData);
          resolve({ status: res.statusCode, data: parsed });
        } catch (e) {
          resolve({ status: res.statusCode, data: responseData });
        }
      });
    });

    req.on('error', (err) => {
      reject(err);
    });

    req.write(JSON.stringify(data));
    req.end();
  });
}

async function testMCPServer(baseUrl) {
  const endpoint = `${baseUrl}/mcp`;

  console.log(`Testing MCP server at: ${endpoint}\n`);

  try {
    // Test 1: Initialize
    console.log('1. Testing initialize...');
    const initResponse = await makeRequest(endpoint, {
      jsonrpc: '2.0',
      id: 1,
      method: 'initialize',
      params: {
        protocolVersion: '2024-11-05',
        capabilities: {},
        clientInfo: {
          name: 'test-client',
          version: '1.0.0'
        }
      }
    });

    if (initResponse.status === 200 && initResponse.data.result) {
      console.log('✅ Initialize successful');
      console.log(`   Server: ${initResponse.data.result.serverInfo?.name} v${initResponse.data.result.serverInfo?.version}`);
    } else {
      console.log('❌ Initialize failed');
      console.log(`   Status: ${initResponse.status}`);
      console.log(`   Response: ${JSON.stringify(initResponse.data, null, 2)}`);
    }

    // Test 2: List tools
    console.log('\n2. Testing tools/list...');
    const toolsResponse = await makeRequest(endpoint, {
      jsonrpc: '2.0',
      id: 2,
      method: 'tools/list',
      params: {}
    });

    if (toolsResponse.status === 200 && toolsResponse.data.result?.tools) {
      const tools = toolsResponse.data.result.tools;
      console.log(`✅ Tools list successful - ${tools.length} tools available:`);
      tools.forEach(tool => {
        console.log(`   - ${tool.name}`);
      });
    } else {
      console.log('❌ Tools list failed');
      console.log(`   Status: ${toolsResponse.status}`);
      console.log(`   Response: ${JSON.stringify(toolsResponse.data, null, 2)}`);
    }

    // Test 3: Call a simple tool (get_service_environments)
    console.log('\n3. Testing tools/call (get_service_environments)...');
    const callResponse = await makeRequest(endpoint, {
      jsonrpc: '2.0',
      id: 3,
      method: 'tools/call',
      params: {
        name: 'get_service_environments',
        arguments: {}
      }
    });

    if (callResponse.status === 200) {
      if (callResponse.data.result) {
        console.log('✅ Tool call successful');
        console.log(`   Response length: ${JSON.stringify(callResponse.data.result).length} characters`);
      } else if (callResponse.data.error) {
        console.log('⚠️  Tool call returned error (this may be expected if no credentials are set):');
        console.log(`   Error: ${callResponse.data.error.message}`);
      }
    } else {
      console.log('❌ Tool call failed');
      console.log(`   Status: ${callResponse.status}`);
      console.log(`   Response: ${JSON.stringify(callResponse.data, null, 2)}`);
    }

    console.log('\n🎉 MCP server testing completed!');

  } catch (error) {
    console.error('❌ Test failed with error:', error.message);
    process.exit(1);
  }
}

// Main execution
if (require.main === module) {
  const url = process.argv[2];
  if (!url) {
    console.log('Usage: node test-deployment.js <URL>');
    console.log('Example: node test-deployment.js http://localhost:8787');
    console.log('Example: node test-deployment.js https://your-worker.workers.dev');
    process.exit(1);
  }

  testMCPServer(url);
}