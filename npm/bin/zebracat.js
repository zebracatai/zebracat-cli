#!/usr/bin/env node
// Thin launcher for the native `zebracat` binary.
// Forwards argv, stdio, and the exit code. If the binary is missing (e.g. install
// ran with --ignore-scripts), it is fetched lazily before the first invocation.

"use strict";

const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const binName = process.platform === "win32" ? "zebracat.exe" : "zebracat";
const binPath = path.join(__dirname, binName);

function run() {
  const res = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });
  if (res.error) {
    process.stderr.write(`zebracat: failed to launch binary: ${res.error.message}\n`);
    process.exit(1);
  }
  process.exit(res.status === null ? 1 : res.status);
}

if (fs.existsSync(binPath)) {
  run();
} else {
  require("../install.js")
    .ensureBinary({ quiet: true })
    .then(run)
    .catch((err) => {
      process.stderr.write(
        `zebracat: the CLI binary is not installed and could not be fetched: ${err.message}\n` +
          `Try reinstalling: npm install -g @zebracat/cli\n`
      );
      process.exit(1);
    });
}
