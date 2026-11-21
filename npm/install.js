#!/usr/bin/env node
// Postinstall hook for @zebracat/cli.
//
// Downloads the matching `zebracat` binary from the zebracat-cli GitHub Releases
// (asset naming mirrors install.sh: zebracat_<os>_<arch>.tar.gz / .zip) into this
// package's bin/ directory, checks its SHA-256 against the release's checksums.txt,
// and extracts it. No third-party dependencies: Node built-ins + the system `tar`.
//
// It fails closed, like install.sh: no checksums.txt, no line for the archive, or a
// wrong sum, and nothing is installed. Downloads go through HTTPS_PROXY / HTTP_PROXY
// (or npm's https-proxy / proxy config) and respect NO_PROXY.
//
// The binary is NOT bundled in the npm tarball: it is fetched at install time so
// the package stays tiny and every platform gets a native build. If install is run
// with --ignore-scripts, bin/zebracat.js fetches it lazily on first run via the
// exported ensureBinary().

"use strict";

const fs = require("fs");
const os = require("os");
const path = require("path");
const http = require("http");
const https = require("https");
const tls = require("tls");
const crypto = require("crypto");
const { spawnSync } = require("child_process");

const REPO = "zebracatai/zebracat-cli";
const VERSION = require("./package.json").version; // release tag = v<version>
const TAG = `v${VERSION}`;
const RELEASES = `https://github.com/${REPO}/releases/download/${TAG}`;
const BIN_DIR = path.join(__dirname, "bin");
const TIMEOUT_MS = 60000;

function purple(msg) {
  process.stdout.write(`\x1b[38;5;141m${msg}\x1b[0m\n`);
}

// Map Node's platform/arch to the goreleaser asset naming.
function target(platform = process.platform, arch = process.arch) {
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

function firstSet(env, names) {
  for (const n of names) {
    const v = env[n];
    if (v && v !== "null" && v !== "false") return v;
  }
  return "";
}

// noProxy reports whether NO_PROXY (or npm's noproxy) exempts host.
function noProxy(host, env) {
  const list = firstSet(env, ["NO_PROXY", "no_proxy", "npm_config_noproxy"]);
  if (!list) return false;
  host = host.toLowerCase();
  return list
    .split(/[,\s]+/)
    .filter(Boolean)
    .some((entry) => {
      entry = entry.toLowerCase().replace(/:\d+$/, "");
      if (entry === "*") return true;
      if (entry.startsWith("*.")) entry = entry.slice(1);
      if (entry.startsWith(".")) return host.endsWith(entry) || host === entry.slice(1);
      return host === entry || host.endsWith(`.${entry}`);
    });
}

// proxyFor returns the proxy URL to reach `url` through, or null for a direct
// connection. The same variables npm and curl read.
function proxyFor(url, env = process.env) {
  const u = new URL(url);
  const names =
    u.protocol === "https:"
      ? ["npm_config_https_proxy", "HTTPS_PROXY", "https_proxy", "npm_config_proxy", "HTTP_PROXY", "http_proxy"]
      : ["npm_config_proxy", "HTTP_PROXY", "http_proxy"];
  const p = firstSet(env, names);
  if (!p || noProxy(u.hostname, env)) return null;
  return new URL(p.includes("://") ? p : `http://${p}`);
}

// Where a proxy is named in a message: never its user name or password.
function proxyLabel(proxy) {
  return `${proxy.protocol}//${proxy.host}`;
}

function proxyAuth(proxy) {
  if (!proxy.username) return {};
  const cred = `${decodeURIComponent(proxy.username)}:${decodeURIComponent(proxy.password)}`;
  return { "Proxy-Authorization": `Basic ${Buffer.from(cred).toString("base64")}` };
}

function proxyPort(proxy) {
  return proxy.port || (proxy.protocol === "https:" ? 443 : 80);
}

// open starts one GET for url (direct, or through env's proxy) and calls back with
// the response. https through a proxy tunnels with CONNECT; http through a proxy
// sends the absolute URL to the proxy.
function open(url, env, onResponse, onError) {
  const u = new URL(url);
  const headers = { "User-Agent": `zebracat-cli-npm/${VERSION}` };
  const proxy = proxyFor(url, env);
  let req;

  if (!proxy) {
    req = (u.protocol === "https:" ? https : http).get(u, { headers }, onResponse);
  } else if (u.protocol === "http:") {
    req = (proxy.protocol === "https:" ? https : http).get(
      {
        host: proxy.hostname,
        port: proxyPort(proxy),
        path: u.toString(),
        headers: { ...headers, Host: u.host, ...proxyAuth(proxy) },
      },
      onResponse
    );
  } else {
    const port = u.port || 443;
    const connect = (proxy.protocol === "https:" ? https : http).request({
      host: proxy.hostname,
      port: proxyPort(proxy),
      method: "CONNECT",
      path: `${u.hostname}:${port}`,
      headers: { Host: `${u.hostname}:${port}`, ...proxyAuth(proxy) },
    });
    connect.setTimeout(TIMEOUT_MS, () => connect.destroy(new Error("timed out")));
    connect.on("connect", (res, socket) => {
      if (res.statusCode !== 200) {
        socket.destroy();
        return onError(new Error(`proxy ${proxyLabel(proxy)} refused the tunnel (HTTP ${res.statusCode})`));
      }
      const tunneled = https.get(
        u,
        {
          headers,
          agent: false,
          createConnection: () => tls.connect({ socket, servername: u.hostname }),
        },
        onResponse
      );
      tunneled.setTimeout(TIMEOUT_MS, () => tunneled.destroy(new Error("timed out")));
      tunneled.on("error", onError);
    });
    connect.on("error", (e) => onError(new Error(`proxy ${proxyLabel(proxy)}: ${e.message}`)));
    connect.end();
    return;
  }
  req.setTimeout(TIMEOUT_MS, () => req.destroy(new Error("timed out")));
  req.on("error", (e) => onError(proxy ? new Error(`proxy ${proxyLabel(proxy)}: ${e.message}`) : e));
}

// GET with redirect following (GitHub release downloads 302 to objects.githubusercontent.com).
function get(url, { env = process.env, redirects = 0 } = {}) {
  return new Promise((resolve, reject) => {
    if (redirects > 8) return reject(new Error("too many redirects"));
    open(
      url,
      env,
      (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          res.resume();
          const next = new URL(res.headers.location, url).toString();
          return resolve(get(next, { env, redirects: redirects + 1 }));
        }
        if (res.statusCode !== 200) {
          res.resume();
          return reject(new Error(`HTTP ${res.statusCode} for ${url}`));
        }
        const chunks = [];
        res.on("data", (c) => chunks.push(c));
        res.on("end", () => resolve(Buffer.concat(chunks)));
        res.on("error", reject);
      },
      (e) => reject(new Error(`${url}: ${e.message}`))
    );
  });
}

// verify checks data against its line in a goreleaser checksums.txt
// ("<sha256>  <name>"). A missing line or a wrong sum throws.
function verify(checksums, asset, data) {
  const line = checksums
    .toString("utf8")
    .split("\n")
    .map((l) => l.trim().split(/\s+/))
    .find((parts) => parts.length === 2 && parts[1] === asset);
  if (!line) {
    throw new Error(`checksums.txt has no line for ${asset}; not installing`);
  }
  const got = crypto.createHash("sha256").update(data).digest("hex");
  if (got !== line[0].toLowerCase()) {
    throw new Error(`checksum mismatch for ${asset}; the download was not installed`);
  }
}

// Extract the binary from a downloaded archive using the system `tar`
// (bsdtar/GNU tar handle .tar.gz; bsdtar, bundled on Windows 10+, handles .zip too).
function extract(archivePath, ext, cwd) {
  const args = ext === "zip" ? ["-xf", archivePath] : ["-xzf", archivePath];
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
    throw new Error(`failed to extract ${archivePath} (is 'tar' on your PATH?)`);
  }
}

// ensureBinary installs the release binary into binDir unless it is there.
// `base` (the release download folder), `binDir`, `env`, `platform` and `arch`
// are options for the tests; the installer itself always uses the defaults.
async function ensureBinary({ quiet = false, base = RELEASES, binDir = BIN_DIR, env = process.env, platform, arch } = {}) {
  const { asset, binName, ext } = target(platform, arch);
  const finalPath = path.join(binDir, binName);

  if (fs.existsSync(finalPath)) return finalPath;

  fs.mkdirSync(binDir, { recursive: true });
  if (!quiet) purple(`🦓 Fetching the Zebracat CLI ${TAG} (${asset})…`);

  const [archive, checksums] = await Promise.all([
    get(`${base}/${asset}`, { env }).catch((e) => {
      throw new Error(`download failed: ${e.message}`);
    }),
    // No checksums.txt, no install: a release without one (or a blocked
    // download of it) must not slip an unchecked binary in.
    get(`${base}/checksums.txt`, { env }).catch((e) => {
      throw new Error(`could not download checksums.txt, so nothing was installed: ${e.message}`);
    }),
  ]);
  verify(checksums, asset, archive);

  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "zebracat-"));
  try {
    const archivePath = path.join(tmp, asset);
    fs.writeFileSync(archivePath, archive);
    extract(archivePath, ext, tmp);

    const extracted = path.join(tmp, binName);
    if (!fs.existsSync(extracted)) {
      throw new Error(`archive did not contain ${binName}`);
    }
    // Copy beside the final name, then rename: a cut copy never leaves a
    // half binary for the launcher to run.
    const partial = path.join(binDir, `.${binName}.partial`);
    fs.copyFileSync(extracted, partial);
    fs.chmodSync(partial, 0o755);
    fs.renameSync(partial, finalPath);
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }

  if (!quiet) purple(`✓ Checksum verified. Zebracat CLI ready. Run: zebracat auth login`);
  return finalPath;
}

// main is the postinstall: a failed install exits non-zero with one clear line,
// so npm shows it now instead of the first run failing later.
async function main(opts = {}) {
  try {
    await ensureBinary(opts);
    return 0;
  } catch (err) {
    process.stderr.write(
      `\x1b[31mzebracat: install failed:\x1b[0m ${err.message}\n` +
        `Check your network or proxy (HTTPS_PROXY) and install again, or use: curl -fsSL https://cli.zebracat.ai | sh\n`
    );
    return 1;
  }
}

module.exports = { ensureBinary, main, proxyFor, verify, target };

if (require.main === module) {
  main().then((code) => process.exit(code));
}
