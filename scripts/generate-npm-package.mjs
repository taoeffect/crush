#!/usr/bin/env node
import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const targets = [
  { key: "linux-x64", suffix: "Linux_x86_64" },
  { key: "linux-arm64", suffix: "Linux_arm64" },
  { key: "darwin-x64", suffix: "Darwin_x86_64" },
  { key: "darwin-arm64", suffix: "Darwin_arm64" },
];

function parseArgs(argv) {
  const args = new Map();
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (!arg.startsWith("--")) {
      throw new Error(`Unexpected argument: ${arg}`);
    }
    const [rawName, inlineValue] = arg.slice(2).split("=", 2);
    const value = inlineValue ?? argv[++i];
    if (!rawName || !value || value.startsWith("--")) {
      throw new Error(`Missing value for --${rawName}`);
    }
    args.set(rawName, value);
  }
  return args;
}

function getOption(args, name, envName, fallback) {
  return args.get(name) ?? process.env[envName] ?? fallback;
}

async function readTemplateVersion() {
  const packageJSONPath = join(repoRoot, "npm", "package.json");
  const packageJSON = JSON.parse(await readFile(packageJSONPath, "utf8"));
  return packageJSON.version;
}

function normalizeVersion(version) {
  if (!version) {
    throw new Error("Missing version. Pass --version, set VERSION, or set npm/package.json version.");
  }
  const normalized = version.replace(/^v/, "");
  if (!/^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(normalized)) {
    throw new Error(`Invalid version: ${version}`);
  }
  return normalized;
}

function normalizeTag(tag, version) {
  const normalized = tag || `v${version}`;
  if (!/^v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(normalized)) {
    throw new Error(`Invalid tag: ${normalized}`);
  }
  return normalized;
}

function parseChecksums(contents) {
  const checksums = new Map();
  for (const [index, rawLine] of contents.split(/\r?\n/).entries()) {
    const line = rawLine.trim();
    if (line === "") {
      continue;
    }
    const match = line.match(/^([a-f0-9]{64})\s+\*?(.+)$/i);
    if (!match) {
      throw new Error(`Malformed checksum line ${index + 1}: ${rawLine}`);
    }
    checksums.set(basename(match[2]), match[1].toLowerCase());
  }
  return checksums;
}

function archiveMetadata({ version, tag, repo, checksums }) {
  const archives = {};
  for (const target of targets) {
    const name = `crush_${version}_${target.suffix}.tar.gz`;
    const digest = checksums.get(name);
    if (!digest) {
      throw new Error(`Missing checksum for ${name}`);
    }
    const wrappedIn = name.replace(/\.tar\.gz$/, "");
    archives[target.key] = {
      name,
      url: `https://github.com/${repo}/releases/download/${tag}/${name}`,
      checksum: {
        algorithm: "sha256",
        digest,
      },
      wrappedIn,
      bin: "crush",
    };
  }
  return archives;
}

async function copyPackageFiles(outputDir) {
  const npmDir = join(repoRoot, "npm");
  const files = ["package.json", "install.js", "run-crush.js", "lib.js"];
  for (const file of files) {
    await cp(join(npmDir, file), join(outputDir, file));
  }
  await cp(join(repoRoot, "README.md"), join(outputDir, "README.md"));
  await cp(join(repoRoot, "LICENSE.md"), join(outputDir, "LICENSE.md"));
}

function assertSafeOutputDir(outputDir) {
  const resolved = resolve(outputDir);
  if (resolved === repoRoot || resolved === dirname(repoRoot) || resolved === "/") {
    throw new Error(`Refusing to remove unsafe output directory: ${resolved}`);
  }
  return resolved;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const version = normalizeVersion(getOption(args, "version", "VERSION", await readTemplateVersion()));
  const tag = normalizeTag(getOption(args, "tag", "TAG"), version);
  const repo = getOption(args, "repo", "GITHUB_REPOSITORY", "taoeffect/crush");
  const distDir = resolve(getOption(args, "dist", "DIST_DIR", join(repoRoot, "dist")));
  const outputDir = assertSafeOutputDir(getOption(args, "out", "NPM_PACKAGE_DIR", join(repoRoot, ".release", "npm-package")));

  const checksums = parseChecksums(await readFile(join(distDir, "checksums.txt"), "utf8"));
  const archives = archiveMetadata({ checksums, repo, tag, version });

  await rm(outputDir, { force: true, recursive: true });
  await mkdir(outputDir, { recursive: true });
  await copyPackageFiles(outputDir);

  const packageJSONPath = join(outputDir, "package.json");
  const packageJSON = JSON.parse(await readFile(packageJSONPath, "utf8"));
  packageJSON.version = version;
  packageJSON.crush = {
    repo,
    archives,
  };
  await writeFile(packageJSONPath, `${JSON.stringify(packageJSON, null, 2)}\n`);

  console.log(`Generated npm package for ${packageJSON.name}@${version} in ${outputDir}`);
}

main().catch((error) => {
  console.error(`Failed to generate npm package: ${error.message}`);
  process.exit(1);
});
