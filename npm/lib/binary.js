const fs = require('fs');
const path = require('path');
const os = require('os');
const https = require('https');
const crypto = require('crypto');
const zlib = require('zlib');
const { execFileSync } = require('child_process');

const pkg = require('../package.json');

const REPO = 'seanpham99/dbtools';
const VERSION_RE = /^\d+\.\d+\.\d+$/;

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
  if (!VERSION_RE.test(version)) {
    throw new Error(`Invalid dbtools version: ${JSON.stringify(version)}`);
  }
  const base = process.env.XDG_CACHE_HOME || path.join(os.homedir(), '.cache');
  return path.join(base, 'dbtools', `v${version}`);
}

function isValidVersion(version) {
  return VERSION_RE.test(version);
}

// HTTPS only: a redirect to any non-https URL is refused, so a MITM cannot
// downgrade the download to plaintext.
function downloadFile(url) {
  return new Promise((resolve, reject) => {
    https.get(url, { headers: { 'User-Agent': 'dbtools-npx-installer' } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        if (!res.headers.location.startsWith('https://')) {
          res.resume();
          return reject(new Error(`Refusing non-HTTPS redirect to ${res.headers.location}`));
        }
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

function sha256(buffer) {
  return crypto.createHash('sha256').update(buffer).digest('hex');
}

// checksums.txt is goreleaser's "<sha256>  <filename>" listing for the release.
// Verification fails closed: no checksum entry or a mismatch aborts the install.
function verifyChecksum(buffer, checksumsTxt, archiveName) {
  const lines = checksumsTxt.toString('utf8').split('\n');
  for (const line of lines) {
    const match = line.match(/^([0-9a-fA-F]{64})\s+\*?(.+)$/);
    if (!match) continue;
    if (path.basename(match[2].trim()) === archiveName) {
      const expected = match[1].toLowerCase();
      const actual = sha256(buffer);
      if (actual !== expected) {
        throw new Error(`Checksum mismatch for ${archiveName}: expected ${expected}, got ${actual}`);
      }
      return;
    }
  }
  throw new Error(`No checksum found for ${archiveName} in checksums.txt`);
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

// Atomic install: write to a temp file next to the target, then rename so a
// partial download never becomes the cached executable.
function installBinary(binData, cachedBin, platform) {
  const tempBin = `${cachedBin}.tmp`;
  fs.writeFileSync(tempBin, binData);
  if (platform !== 'win32') {
    fs.chmodSync(tempBin, 0o755);
  }
  fs.renameSync(tempBin, cachedBin);
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
  const releaseBase = `https://github.com/${REPO}/releases/download/v${version}`;
  const downloadUrl = `${releaseBase}/${archiveName}`;

  process.stderr.write(`Downloading dbtools v${version} for ${osName}/${archName}...\n`);
  const archiveBuf = await downloadFile(downloadUrl);

  const checksumsTxt = await downloadFile(`${releaseBase}/checksums.txt`);
  verifyChecksum(archiveBuf, checksumsTxt, archiveName);

  if (ext === 'tar.gz') {
    const binData = extractTarGz(archiveBuf, binName);
    installBinary(binData, cachedBin, platform);
  } else {
    // Windows zip handling. Paths are embedded in a PowerShell command string;
    // version validation and the fixed cache layout keep quotes out of them.
    const tempZip = path.join(cacheDir, archiveName);
    fs.writeFileSync(tempZip, archiveBuf);
    execFileSync('powershell', ['-NoProfile', '-NonInteractive', '-command',
      `Expand-Archive -Path '${tempZip}' -DestinationPath '${cacheDir}' -Force`]);
    try { fs.unlinkSync(tempZip); } catch (_) {}

    const binData = fs.readFileSync(path.join(cacheDir, binName));
    installBinary(binData, cachedBin, platform);
  }

  return cachedBin;
}

module.exports = {
  getBinaryPath,
  getPlatformInfo,
  extractTarGz,
  isValidVersion,
  verifyChecksum,
  sha256,
  downloadFile
};
