#!/usr/bin/env node

const { spawn } = require('child_process');
const path = require('path');

// Get the path to the binary
const binaryName = process.platform === 'win32' ? 'last9-mcp-server.exe' : 'last9-mcp-server';
const binaryPath = path.join(__dirname, '..', 'dist', binaryName);

// Spawn the binary with all arguments passed through
const proc = spawn(binaryPath, process.argv.slice(2), {
  stdio: 'inherit',
  env: process.env
});

// Handle process exit
proc.on('exit', (code) => {
  process.exit(code);
}); 