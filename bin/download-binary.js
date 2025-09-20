const https = require('https');
const fs = require('fs');
const path = require('path');
const { execSync, execFileSync } = require('child_process');
const os = require('os');

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
console.log(`Extracting to: ${binaryPath}`);

// Create temporary file for the archive
const tempArchivePath = path.join(os.tmpdir(), `last9-mcp-server-${Date.now()}${platform === 'win32' ? '.zip' : '.tar.gz'}`);
const file = fs.createWriteStream(tempArchivePath);

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

        // Extract the archive
        try {
          if (platform === 'win32') {
            // Extract zip file (requires unzip or similar)
            execSync(`cd "${distDir}" && unzip -o "${tempArchivePath}"`);
          } else {
            // Extract tar.gz file
            execSync(`tar -xzf "${tempArchivePath}" -C "${distDir}"`);
          }
          console.log('Archive extracted');

          // Clean up temporary file
          fs.unlinkSync(tempArchivePath);

          // Make binary executable on Unix-like systems
          if (platform !== 'win32') {
            execFileSync('chmod', ['+x', binaryPath]);
            console.log('Made binary executable');
          }

          resolve();
        } catch (err) {
          // Clean up on error
          try { fs.unlinkSync(tempArchivePath); } catch {}
          reject(new Error(`Failed to extract or setup binary: ${err.message}`));
        }
      });
    }).on('error', (err) => {
      fs.unlink(tempArchivePath, () => {}); // Delete the temp file async
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