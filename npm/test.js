const assert = require('assert');
const path = require('path');
const zlib = require('zlib');
const { execSync } = require('child_process');
const { getPlatformInfo, extractTarGz } = require('./lib/binary.js');

console.log('Testing dbtools-cli installer package...');

// 1. Test platform detection
const info = getPlatformInfo();
assert(info.osName, 'osName should be defined');
assert(info.archName, 'archName should be defined');
assert(info.ext, 'ext should be defined');
assert(info.binName, 'binName should be defined');
console.log(`✓ Platform detected: ${info.osName} (${info.archName}) -> ${info.binName}`);

// 2. Test tar.gz extractor
function createMockTarGz(filename, content) {
  const header = Buffer.alloc(512, 0);
  header.write(filename, 0, 100, 'utf8');
  const sizeOctal = content.length.toString(8).padStart(11, '0') + ' ';
  header.write(sizeOctal, 124, 12, 'utf8');
  header[156] = 48; // '0' for normal file

  const contentPadded = Buffer.alloc(Math.ceil(content.length / 512) * 512, 0);
  Buffer.from(content).copy(contentPadded);

  const endBlock = Buffer.alloc(1024, 0);
  const tarBuf = Buffer.concat([header, contentPadded, endBlock]);
  return zlib.gzipSync(tarBuf);
}

const mockArchive = createMockTarGz('dbtools', 'binary-content-mock');
const extracted = extractTarGz(mockArchive, 'dbtools');
assert.strictEqual(extracted.toString(), 'binary-content-mock');
console.log('✓ extractTarGz successfully extracted binary from mock archive');

// 3. Test CLI wrapper invocation with DBTOOLS_BINARY_PATH override
const projectRoot = path.join(__dirname, '..');
const localBin = path.join(projectRoot, 'bin', 'dbtools-local-test');
execSync(`go build -o "${localBin}" .`, { cwd: projectRoot });

const out = execSync(`node bin/dbtools.js --help`, {
  cwd: __dirname,
  env: {
    ...process.env,
    DBTOOLS_BINARY_PATH: localBin
  }
}).toString();

assert(out.includes('dbtools manages MSSQL/Postgres schema migrations'), 'CLI output should match dbtools help');
console.log('✓ bin/dbtools.js execution via DBTOOLS_BINARY_PATH passed');

// Clean up
try { require('fs').unlinkSync(localBin); } catch (_) {}

console.log('All npm package tests passed!');
