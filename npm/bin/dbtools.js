#!/usr/bin/env node

const { spawn } = require('child_process');
const { getBinaryPath } = require('../lib/binary.js');

async function main() {
  try {
    const binPath = await getBinaryPath();
    const child = spawn(binPath, process.argv.slice(2), {
      stdio: 'inherit',
      env: process.env
    });

    child.on('error', (err) => {
      console.error(`Failed to start dbtools: ${err.message}`);
      process.exit(1);
    });

    child.on('close', (code) => {
      process.exit(code ?? 0);
    });
  } catch (err) {
    console.error(`dbtools installer error: ${err.message}`);
    process.exit(1);
  }
}

main();
