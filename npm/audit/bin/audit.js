#!/usr/bin/env node
const { execFileSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const REPO = "try-loopers/loopers-oss";
const cacheDir = path.join(os.homedir(), ".cache", "loopers", "bin");
const isWin = process.platform === "win32";
const binName = isWin ? "loopers.exe" : "loopers";
const binPath = path.join(cacheDir, binName);

function getAssetUrl() {
  const plat = process.platform === "darwin" ? "Darwin" : isWin ? "Windows" : "Linux";
  const arch = process.arch === "arm64" ? "arm64" : "x86_64";
  const ext = isWin ? "zip" : "tar.gz";
  return `https://github.com/${REPO}/releases/latest/download/loopers_${plat}_${arch}.${ext}`;
}

if (!fs.existsSync(binPath)) {
  fs.mkdirSync(cacheDir, { recursive: true });
  const assetUrl = getAssetUrl();
  const tmpArchive = path.join(os.tmpdir(), isWin ? "loopers.zip" : "loopers.tar.gz");

  const curlBin = isWin ? "curl.exe" : "curl";
  const tarBin = isWin ? "tar.exe" : "tar";

  try {
    process.stderr.write(`  › Fetching Loopers X-Ray scanner...\n`);
    execFileSync(curlBin, ["-fsSL", assetUrl, "-o", tmpArchive], { stdio: "inherit" });
    execFileSync(tarBin, ["-xf", tmpArchive, "-C", cacheDir], { stdio: "inherit" });
    try { fs.unlinkSync(tmpArchive); } catch (_) {}
    if (!isWin) {
      try { fs.chmodSync(binPath, 0o755); } catch (_) {}
    }
  } catch (err) {
    process.stderr.write(`Failed to download binary: ${err.message}\n`);
    process.exit(1);
  }
}

try {
  execFileSync(binPath, ["audit", ...process.argv.slice(2)], { stdio: "inherit" });
} catch (e) {
  process.exit(e.status || 1);
}
