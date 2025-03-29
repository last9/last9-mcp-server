const https = require('https');
const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

// Get version from package.json
const version = require('../package.json').version;
const platform = process.platform;
const arch = process.arch;

// Map Node.js arch to Go arch
const archMap = {
  'x64': 'x86_64',  // Changed to match .goreleaser.yml naming
  'arm64': 'arm64',
};

// Map platform to GoReleaser OS naming
const osMap = {
  'darwin': 'Darwin',
  'linux': 'Linux',
  'win32': 'Windows'
};

const binaryName = platform === 'win32' ? 'last9-mcp-server.exe' : 'last9-mcp-server';
const distDir = path.join(__dirname, '..', 'dist');
const binaryPath = path.join(distDir, binaryName);

// Create dist directory if it doesn't exist
if (!fs.existsSync(distDir)) {
  fs.mkdirSync(distDir, { recursive: true });
}

// Match the format in .goreleaser.yml name_template
const downloadUrl = `https://github.com/last9/last9-mcp-server/releases/download/v${version}/last9-mcp-server_${osMap[platform]}_${archMap[arch]}${platform === 'win32' ? '.zip' : '.tar.gz'}`;

console.log(`Downloading from: ${downloadUrl}`);
console.log(`Saving to: ${binaryPath}`);

const file = fs.createWriteStream(binaryPath);

const download = (url) => {
  return new Promise((resolve, reject) => {
    https.get(url, (response) => {
      if (response.statusCode === 302) {
        // Handle redirect
        download(response.headers.location)
          .then(resolve)
          .catch(reject);
        return;
      }

      if (response.statusCode !== 200) {
        reject(new Error(`Failed to download: ${response.statusCode} ${response.statusMessage}`));
        return;
      }

      response.pipe(file);

      file.on('finish', () => {
        file.close();
        console.log('Download completed');
        
        // Make binary executable on Unix-like systems
        if (platform !== 'win32') {
          try {
            execSync(`chmod +x ${binaryPath}`);
            console.log('Made binary executable');
          } catch (err) {
            reject(new Error(`Failed to make binary executable: ${err.message}`));
            return;
          }
        }
        
        resolve();
      });
    }).on('error', (err) => {
      fs.unlink(binaryPath, () => {}); // Delete the file async
      reject(new Error(`Download failed: ${err.message}`));
    });
  });
};

download(downloadUrl)
  .then(() => {
    console.log('Installation completed successfully');
  })
  .catch((err) => {
    console.error('Installation failed:', err.message);
    process.exit(1);
  }); 