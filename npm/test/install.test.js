"use strict";

// node --test npm/test
// install.js against a local release server: it installs only a download whose
// sum matches checksums.txt, and it goes through the proxy it is given.

const test = require("node:test");
const assert = require("node:assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const http = require("http");
const net = require("net");
const crypto = require("crypto");
const { spawnSync } = require("child_process");

const { ensureBinary, main, proxyFor } = require("../install.js");

const PLATFORM = "linux";
const ARCH = "x64";
const ASSET = "zebracat_linux_amd64.tar.gz";

function tarball() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zc-src-"));
  fs.writeFileSync(path.join(dir, "zebracat"), "#!/bin/sh\necho installed-binary\n", { mode: 0o755 });
  const out = path.join(dir, ASSET);
  const r = spawnSync("tar", ["-czf", out, "zebracat"], { cwd: dir });
  assert.strictEqual(r.status, 0, String(r.stderr));
  return fs.readFileSync(out);
}

const ARCHIVE = tarball();
const SUM = crypto.createHash("sha256").update(ARCHIVE).digest("hex");

// release serves /dl/<asset> and /dl/checksums.txt per mode.
function release(mode) {
  const hits = [];
  const srv = http.createServer((req, res) => {
    hits.push(req.url);
    if (req.url === `/dl/${ASSET}`) return res.end(ARCHIVE);
    if (req.url === "/dl/checksums.txt") {
      if (mode === "missing") {
        res.statusCode = 404;
        return res.end("not found");
      }
      if (mode === "bad") return res.end(`${"0".repeat(64)}  ${ASSET}\n`);
      if (mode === "other") return res.end(`${SUM}  zebracat_plan9_mips.tar.gz\n`);
      return res.end(`${SUM}  ${ASSET}\n`);
    }
    res.statusCode = 404;
    res.end();
  });
  return new Promise((resolve) =>
    srv.listen(0, "127.0.0.1", () => resolve({ srv, hits, base: `http://127.0.0.1:${srv.address().port}/dl` }))
  );
}

// forwardProxy answers absolute-URI GETs by fetching them itself, and CONNECT
// with `connectStatus` (no real tunnel: the tests never leave this machine).
function forwardProxy(connectStatus = 403) {
  const seen = [];
  const srv = http.createServer((req, res) => {
    seen.push(`${req.method} ${req.url}`);
    http
      .get(req.url, (up) => {
        res.writeHead(up.statusCode, up.headers);
        up.pipe(res);
      })
      .on("error", () => {
        res.statusCode = 502;
        res.end();
      });
  });
  srv.on("connect", (req, socket) => {
    seen.push(`CONNECT ${req.url}`);
    socket.end(`HTTP/1.1 ${connectStatus} Forbidden\r\n\r\n`);
  });
  return new Promise((resolve) =>
    srv.listen(0, "127.0.0.1", () => resolve({ srv, seen, url: `http://127.0.0.1:${srv.address().port}` }))
  );
}

function deadPort() {
  return new Promise((resolve) => {
    const s = net.createServer().listen(0, "127.0.0.1", () => {
      const port = s.address().port;
      s.close(() => resolve(port));
    });
  });
}

function tmpBin() {
  return fs.mkdtempSync(path.join(os.tmpdir(), "zc-bin-"));
}

const opts = (base, binDir, env = {}) => ({ quiet: true, base, binDir, env, platform: PLATFORM, arch: ARCH });

test("a matching sum installs the binary", async () => {
  const r = await release("good");
  try {
    const bin = tmpBin();
    const p = await ensureBinary(opts(r.base, bin));
    assert.strictEqual(p, path.join(bin, "zebracat"));
    assert.match(fs.readFileSync(p, "utf8"), /installed-binary/);
    assert.deepStrictEqual(fs.readdirSync(bin), ["zebracat"]);
  } finally {
    r.srv.close();
  }
});

for (const [mode, why] of [
  ["bad", /checksum mismatch/],
  ["other", /no line for zebracat_linux_amd64\.tar\.gz/],
  ["missing", /could not download checksums\.txt/],
]) {
  test(`checksums.txt ${mode}: nothing is installed`, async () => {
    const r = await release(mode);
    try {
      const bin = tmpBin();
      await assert.rejects(ensureBinary(opts(r.base, bin)), why);
      assert.deepStrictEqual(fs.readdirSync(bin), [], "no binary may be left behind");
    } finally {
      r.srv.close();
    }
  });
}

test("HTTP_PROXY is used for the download", async () => {
  const r = await release("good");
  const p = await forwardProxy();
  try {
    const bin = tmpBin();
    await ensureBinary(opts(r.base, bin, { HTTP_PROXY: p.url }));
    assert.ok(p.seen.includes(`GET ${r.base}/${ASSET}`), `proxy saw: ${p.seen}`);
    assert.ok(p.seen.includes(`GET ${r.base}/checksums.txt`), `proxy saw: ${p.seen}`);
    assert.ok(fs.existsSync(path.join(bin, "zebracat")));
  } finally {
    r.srv.close();
    p.srv.close();
  }
});

test("a dead proxy fails the install instead of going around it", async () => {
  const r = await release("good");
  try {
    const bin = tmpBin();
    const port = await deadPort();
    await assert.rejects(
      ensureBinary(opts(r.base, bin, { npm_config_proxy: `http://user:secret@127.0.0.1:${port}` })),
      (e) => /proxy http:\/\/127\.0\.0\.1:\d+/.test(e.message) && !e.message.includes("secret")
    );
    assert.deepStrictEqual(r.hits, [], "the release server must not be reached directly");
    assert.deepStrictEqual(fs.readdirSync(bin), []);
  } finally {
    r.srv.close();
  }
});

test("https downloads tunnel through HTTPS_PROXY with CONNECT", async () => {
  const p = await forwardProxy(403);
  try {
    const bin = tmpBin();
    await assert.rejects(
      ensureBinary(opts("https://github.example/dl", bin, { HTTPS_PROXY: p.url })),
      /refused the tunnel \(HTTP 403\)/
    );
    assert.ok(p.seen.includes("CONNECT github.example:443"), `proxy saw: ${p.seen}`);
  } finally {
    p.srv.close();
  }
});

test("NO_PROXY and npm's proxy settings pick the route", () => {
  const url = "https://github.com/zebracatai/zebracat-cli/releases/download/v1.0.0/checksums.txt";
  assert.strictEqual(proxyFor(url, {}), null);
  assert.strictEqual(proxyFor(url, { HTTPS_PROXY: "http://p:3128" }).host, "p:3128");
  assert.strictEqual(proxyFor(url, { https_proxy: "p:3128" }).host, "p:3128");
  assert.strictEqual(proxyFor(url, { npm_config_https_proxy: "http://npm:8080", HTTPS_PROXY: "http://env:1" }).host, "npm:8080");
  assert.strictEqual(proxyFor(url, { HTTPS_PROXY: "http://p:3128", NO_PROXY: ".github.com" }), null);
  assert.strictEqual(proxyFor(url, { HTTPS_PROXY: "http://p:3128", no_proxy: "github.com" }), null);
  assert.strictEqual(proxyFor(url, { HTTPS_PROXY: "http://p:3128", NO_PROXY: "*" }), null);
  assert.strictEqual(proxyFor(url, { HTTPS_PROXY: "http://p:3128", NO_PROXY: "example.com" }).host, "p:3128");
  assert.strictEqual(proxyFor("http://x.test/a", { HTTPS_PROXY: "http://p:3128" }), null, "http targets read HTTP_PROXY only");
});

test("postinstall exits 1 with one clear line when the download fails", async () => {
  const port = await deadPort();
  const bin = tmpBin();
  const writes = [];
  const orig = process.stderr.write;
  process.stderr.write = (s) => (writes.push(String(s)), true);
  let code;
  try {
    code = await main(opts(`http://127.0.0.1:${port}/dl`, bin));
  } finally {
    process.stderr.write = orig;
  }
  assert.strictEqual(code, 1);
  const out = writes.join("");
  assert.match(out, /zebracat: install failed:/);
  assert.match(out, /HTTPS_PROXY/);
  assert.deepStrictEqual(fs.readdirSync(bin), []);
});
