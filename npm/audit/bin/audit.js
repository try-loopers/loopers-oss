#!/usr/bin/env node
// File: npm/audit/bin/audit.js
const { execFileSync } = require("child_process");
const fs = require("fs");
const os = require("os");
const path = require("path");

const REPO = "try-loopers/loopers-oss";
const cacheDir = path.join(os.homedir(), ".cache", "loopers", "bin");
const binName = process.platform === "win32" ? "loopers.exe" : "loopers";
const binPath = path.join(cacheDir, binName);

function getAssetUrl() {
  const plat = process.platform === "darwin" ? "Darwin" : process.platform === "win32" ? "Windows" : "Linux";
  const arch = process.arch === "arm64" ? "arm64" : "x86_64";
  const ext = process.platform === "win32" ? "zip" : "tar.gz";
  return `https://github.com/${REPO}/releases/latest/download/loopers_${plat}_${arch}.${ext}`;
}

if (!fs.existsSync(binPath)) {
  fs.mkdirSync(cacheDir, { recursive: true });
  const shCmd = `curl -fsSL "${getAssetUrl()}" | tar -xz -C "${cacheDir}" loopers && chmod +x "${binPath}"`;
  execFileSync("sh", ["-c", shCmd], { stdio: "inherit" });
}

try {
  execFileSync(binPath, ["audit", ...process.argv.slice(2)], { stdio: "inherit" });
} catch (e) {
  process.exit(e.status || 1);
}

