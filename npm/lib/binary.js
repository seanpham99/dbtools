const fs = require('fs');
const path = require('path');
const os = require('os');
const https = require('https');
const http = require('http');
const zlib = require('zlib');
const { execSync } = require('child_process');

const pkg = require('../package.json');

const REPO = 'seanpham99/dbtools';

function getPlatformInfo() {
  const platform = process.platform;
  const arch = process.arch;

  let osName = '';
  if (platform === 'linux') {
    osName = 'linux';
  } else if (platform === 'darwin') {
    osName = 'darwin';
  } else if (platform === 'win32') {
    osName = 'windows';
  } else {
    throw new Error(`Unsupported OS: ${platform}`);
  }

  let archName = '';
  if (arch === 'x64') {
    archName = 'amd64';
  } else if (arch === 'arm64') {
    archName = 'arm64';
  } else {
    throw new Error(`Unsupported architecture: ${arch}`);
  }

  const ext = platform === 'win32' ? 'zip' : 'tar.gz';
  const binName = platform === 'win32' ? 'dbtools.exe' : 'dbtools';

  return { osName, archName, ext, binName, platform };
}

function getCacheDir(version) {
  const base = process.env.XDG_CACHE_HOME || path.join(os.homedir(), '.cache');
  return path.join(base, 'dbtools', `v${version}`);
}

function downloadFile(url) {
  return new Promise((resolve, reject) => {
    const client = url.startsWith('https') ? https : http;
    client.get(url, { headers: { 'User-Agent': 'dbtools-npx-installer' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        return downloadFile(res.headers.location).then(resolve).catch(reject);
      }
      if (res.statusCode !== 200) {
        return reject(new Error(`Failed to download ${url}: HTTP ${res.statusCode}`));
      }
      const chunks = [];
      res.on('data', (chunk) => chunks.push(chunk));
      res.on('end', () => resolve(Buffer.concat(chunks)));
      res.on('error', reject);
    }).on('error', reject);
  });
}

function extractTarGz(buffer, targetBinaryName) {
  const uncompressed = zlib.gunzipSync(buffer);
  let offset = 0;

  while (offset < uncompressed.length) {
    const header = uncompressed.subarray(offset, offset + 512);
    if (header.every((b) => b === 0)) {
      break;
    }

    // Name is 100 bytes null-terminated
    let nameEnd = 0;
    while (nameEnd < 100 && header[nameEnd] !== 0) {
      nameEnd++;
    }
    const name = header.subarray(0, nameEnd).toString('utf8');

    // Size is 12 bytes octal string at offset 124
    const sizeStr = header.subarray(124, 136).toString('utf8').trim().replace(/\0/g, '');
    const size = parseInt(sizeStr, 8) || 0;

    offset += 512;
    const fileData = uncompressed.subarray(offset, offset + size);

    if (name === targetBinaryName || path.basename(name) === targetBinaryName) {
      return fileData;
    }

    // Tar blocks are padded to 512 bytes
    offset += Math.ceil(size / 512) * 512;
  }

  throw new Error(`Binary ${targetBinaryName} not found in archive`);
}

async function getBinaryPath() {
  if (process.env.DBTOOLS_BINARY_PATH && fs.existsSync(process.env.DBTOOLS_BINARY_PATH)) {
    return process.env.DBTOOLS_BINARY_PATH;
  }

  const version = process.env.DBTOOLS_VERSION || pkg.binaryVersion || pkg.version;
  const { osName, archName, ext, binName, platform } = getPlatformInfo();

  const cacheDir = getCacheDir(version);
  const cachedBin = path.join(cacheDir, binName);

  if (fs.existsSync(cachedBin)) {
    return cachedBin;
  }

  fs.mkdirSync(cacheDir, { recursive: true });

  const archiveName = `dbtools_${version}_${osName}_${archName}.${ext}`;
  const downloadUrl = `https://github.com/${REPO}/releases/download/v${version}/${archiveName}`;

  process.stderr.write(`Downloading dbtools v${version} for ${osName}/${archName}...\n`);
  const archiveBuf = await downloadFile(downloadUrl);

  if (ext === 'tar.gz') {
    const binData = extractTarGz(archiveBuf, binName);
    fs.writeFileSync(cachedBin, binData);
  } else {
    // Windows zip handling
    const tempZip = path.join(cacheDir, archiveName);
    fs.writeFileSync(tempZip, archiveBuf);
    execSync(`powershell -command "Expand-Archive -Path '${tempZip}' -DestinationPath '${cacheDir}' -Force"`);
    try { fs.unlinkSync(tempZip); } catch (_) {}
  }

  if (platform !== 'win32') {
    fs.chmodSync(cachedBin, 0o755);
  }

  return cachedBin;
}

module.exports = {
  getBinaryPath,
  getPlatformInfo,
  extractTarGz
};
