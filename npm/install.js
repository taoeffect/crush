import { createWriteStream } from "node:fs";
import { mkdtemp, rename, stat, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";

import { ProxyAgent } from "proxy-agent";
import tar from "tar";

import {
  assertSafeArchivePath,
  binaryPath,
  makeBinaryExecutable,
  prepareVendorDir,
  readPackageMetadata,
  sha256File,
  validateArchiveMetadata,
  vendorDir,
} from "./lib.js";

async function downloadArchive(url, destination) {
  const response = await fetch(url, {
    agent: new ProxyAgent(),
    redirect: "follow",
  });

  if (!response.ok) {
    throw new Error(`Failed to download ${url}: ${response.status} ${response.statusText}`);
  }
  if (!response.body) {
    throw new Error(`Failed to download ${url}: response body is empty`);
  }

  await pipeline(Readable.fromWeb(response.body), createWriteStream(destination, { mode: 0o600 }));
}

async function extractBinary(archivePath, archive) {
  await tar.x({
    cwd: vendorDir,
    file: archivePath,
    filter(path, entry) {
      assertSafeArchivePath(path, archive);
      return path === `${archive.wrappedIn}/${archive.bin}` && entry.type === "File";
    },
    strip: 1,
  });

  await rename(join(vendorDir, archive.bin), binaryPath);
  await makeBinaryExecutable();
}

async function install() {
  const { archive, key } = await readPackageMetadata();
  validateArchiveMetadata(archive);

  const tempDir = await mkdtemp(join(tmpdir(), "crush-npm-"));
  const archivePath = join(tempDir, archive.name);

  try {
    console.log(`Downloading Crush for ${key}...`);
    await downloadArchive(archive.url, archivePath);

    const digest = await sha256File(archivePath);
    if (digest.toLowerCase() !== archive.checksum.digest.toLowerCase()) {
      throw new Error(`Checksum mismatch for ${archive.name}: expected ${archive.checksum.digest}, got ${digest}`);
    }

    await prepareVendorDir();
    await extractBinary(archivePath, archive);
    await stat(binaryPath);
    console.log(`Installed Crush to ${binaryPath}`);
  } finally {
    await rm(tempDir, { force: true, recursive: true });
  }
}

install().catch((error) => {
  console.error(`Failed to install Crush: ${error.message}`);
  process.exit(1);
});
