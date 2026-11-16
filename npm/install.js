#!/usr/bin/env node
// Postinstall hook for @zebracat/cli.
//
// Downloads the matching `zebracat` binary from the zebracat-cli GitHub Releases
// (asset naming mirrors install.sh: zebracat_<os>_<arch>.tar.gz / .zip) into this
// package's bin/ directory, verifies its SHA-256 against the release checksums.txt,
// and extracts it. No third-party dependencies — Node built-ins + the system `tar`.
//
// The binary is NOT bundled in the npm tarball: it is fetched at install time so
// the package stays tiny and every platform gets a native build. If install is run
// with --ignore-scripts, bin/zebracat.js fetches it lazily on first run via the
// exported ensureBinary().

"use strict";

const fs = require("fs");
const os = require("os");
const path = require("path");
const https = require("https");
const crypto = require("crypto");
const { spawnSync } = require("child_process");

const REPO = "zebracatai/zebracat-cli";
const VERSION = require("./package.json").version; // release tag = v<version>
const TAG = `v${VERSION}`;
const BIN_DIR = path.join(__dirname, "bin");

function purple(msg) {
  process.stdout.write(`\x1b[38;5;141m${msg}\x1b[0m\n`);
}

// Map Node's platform/arch to the goreleaser asset naming.
function target() {
  const platform = process.platform; // 'linux' | 'darwin' | 'win32'
  const arch = process.arch; // 'x64' | 'arm64'

  const goarch = { x64: "amd64", arm64: "arm64" }[arch];
  const goos = { linux: "linux", darwin: "darwin", win32: "windows" }[platform];

  if (!goos || !goarch) {
    throw new Error(
      `unsupported platform: ${platform}/${arch}. ` +
        `Install manually from https://github.com/${REPO}/releases/${TAG}`
    );
  }

  const ext = goos === "windows" ? "zip" : "tar.gz";
  const asset = `zebracat_${goos}_${goarch}.${ext}`;
  const binName = goos === "windows" ? "zebracat.exe" : "zebracat";
  return { asset, binName, ext };
}

// GET with redirect following (GitHub release downloads 302 to objects.githubusercontent.com).
function get(url, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > 8) return reject(new Error("too many redirects"));
    https
      .get(url, { headers: { "User-Agent": "zebracat-cli-npm" } }, (res) => {
        if (
          res.statusCode >= 300 &&
          res.statusCode < 400 &&
          res.headers.location
        ) {
          res.resume();
          const next = new URL(res.headers.location, url).toString();
          return resolve(get(next, redirects + 1));
        }
        if (res.statusCode !== 200) {
          res.resume();
          return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      })
      .on("error", reject);
  });
}

// Extract the binary from a downloaded archive using the system `tar`
// (bsdtar/GNU tar handle .tar.gz; bsdtar — bundled on Windows 10+ — handles .zip too).
function extract(archivePath, ext, cwd) {
  const args =
    ext === "zip"
      ? ["-xf", archivePath] // bsdtar auto-detects zip
      : ["-xzf", archivePath];
  const res = spawnSync("tar", args, { cwd, stdio: "inherit" });
  if (res.status !== 0) {
    // Windows fallback: PowerShell Expand-Archive for .zip
    if (ext === "zip" && process.platform === "win32") {
      const ps = spawnSync(
        "powershell",
        [
          "-NoProfile",
          "-Command",
          `Expand-Archive -Path "${archivePath}" -DestinationPath "${cwd}" -Force`,
        ],
        { stdio: "inherit" }
      );
      if (ps.status === 0) return;
    }
    throw new Error(
      `failed to extract ${archivePath} (is 'tar' on your PATH?)`
    );
  }
}

async function ensureBinary({ quiet = false } = {}) {
  const { asset, binName, ext } = target();
  const finalPath = path.join(BIN_DIR, binName);

  if (fs.existsSync(finalPath)) return finalPath;

  fs.mkdirSync(BIN_DIR, { recursive: true });
  if (!quiet) purple(`🦓 Fetching the Zebracat CLI ${TAG} (${asset})…`);

  const base = `https://github.com/${REPO}/releases/download/${TAG}`;
  const [archive, checksums] = await Promise.all([
    get(`${base}/${asset}`),
    get(`${base}/checksums.txt`).catch(() => null),
  ]);

  // Verify SHA-256 when checksums.txt is available.
  if (checksums) {
    const want = checksums
      .toString("utf8")
      .split("\n")
      .map((l) => l.trim().split(/\s+/))
      .find((parts) => parts[1] === asset);
    if (want) {
      const got = crypto.createHash("sha256").update(archive).digest("hex");
      if (got !== want[0]) {
        throw new Error(
          `checksum mismatch for ${asset}: expected ${want[0]}, got ${got}`
        );
      }
    }
  }

  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "zebracat-"));
  try {
    const archivePath = path.join(tmp, asset);
    fs.writeFileSync(archivePath, archive);
    extract(archivePath, ext, tmp);

    const extracted = path.join(tmp, binName);
    if (!fs.existsSync(extracted)) {
      throw new Error(`archive did not contain ${binName}`);
    }
    fs.copyFileSync(extracted, finalPath);
    fs.chmodSync(finalPath, 0o755);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }

  if (!quiet) purple(`✓ Zebracat CLI ready. Run: zebracat auth login`);
  return finalPath;
}

module.exports = { ensureBinary };

if (require.main === module) {
  ensureBinary().catch((err) => {
    process.stderr.write(
      `\x1b[31mzebracat: could not install the binary:\x1b[0m ${err.message}\n` +
        `It will be fetched on first run instead, or install manually:\n` +
        `  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh | bash\n`
    );
    // Do not hard-fail the whole npm install; bin/zebracat.js retries lazily.
    process.exit(0);
  });
}
