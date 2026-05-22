import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { chmod, mkdir, readFile, rm } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const packageRoot = dirname(fileURLToPath(import.meta.url));
export const vendorDir = join(packageRoot, "vendor", "crush");
export const binaryName = process.platform === "win32" ? "crush.exe" : "crush";
export const binaryPath = join(vendorDir, binaryName);

const platformKeys = new Map([
  ["linux:x64", "linux-x64"],
  ["linux:arm64", "linux-arm64"],
  ["darwin:x64", "darwin-x64"],
  ["darwin:arm64", "darwin-arm64"],
]);

export function platformKey(platform = process.platform, arch = process.arch) {
  const key = platformKeys.get(`${platform}:${arch}`);
  if (!key) {
    throw new Error(`Unsupported platform: ${platform}/${arch}`);
  }
  return key;
}

export async function readPackageMetadata() {
  const packageJSON = JSON.parse(await readFile(join(packageRoot, "package.json"), "utf8"));
  const key = platformKey();
  const archive = packageJSON.crush?.archives?.[key];
  if (!archive) {
    throw new Error(`No Crush archive metadata for platform ${key}`);
  }
  return { archive, key, packageJSON };
}

export async function prepareVendorDir() {
  await rm(vendorDir, { force: true, recursive: true });
  await mkdir(vendorDir, { recursive: true });
}

export async function makeBinaryExecutable(path = binaryPath) {
  if (process.platform !== "win32") {
    await chmod(path, 0o755);
  }
}

export function validateArchiveMetadata(archive) {
  if (!archive || typeof archive !== "object") {
    throw new Error("Missing archive metadata");
  }
  for (const field of ["name", "url", "wrappedIn", "bin"]) {
    if (typeof archive[field] !== "string" || archive[field] === "") {
      throw new Error(`Archive metadata is missing ${field}`);
    }
  }
  if (archive.checksum?.algorithm !== "sha256") {
    throw new Error("Archive checksum algorithm must be sha256");
  }
  if (typeof archive.checksum.digest !== "string" || !/^[a-f0-9]{64}$/i.test(archive.checksum.digest)) {
    throw new Error("Archive checksum digest must be a SHA-256 hex string");
  }
}

export async function sha256File(path) {
  const hash = createHash("sha256");
  await new Promise((resolve, reject) => {
    createReadStream(path)
      .on("error", reject)
      .on("data", (chunk) => hash.update(chunk))
      .on("end", resolve);
  });
  return hash.digest("hex");
}

export function assertSafeArchivePath(path, archive) {
  const expectedPath = `${archive.wrappedIn}/${archive.bin}`;
  if (path === expectedPath) {
    return;
  }
  if (path.startsWith("/") || path.includes("..")) {
    throw new Error(`Unsafe archive entry path: ${path}`);
  }
}
