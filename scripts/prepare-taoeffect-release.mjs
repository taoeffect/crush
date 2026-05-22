#!/usr/bin/env node
import { readFile, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const semverPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$/;

function usage() {
  return `Usage:
  node scripts/prepare-taoeffect-release.mjs [options] <npm-version>

Required:
  <npm-version>
      NPM package version to write into npm/package.json.
      Must NOT start with "v". The script prints the matching Git tag separately.
      Must be a fork prerelease in this form: <upstream-version>-taoeffect.<n>
      Example: 0.10.0-taoeffect.1

Options:
  --check
      Verify that package.json already has <npm-version>. Does not modify files.

  --package-json <path>
      Package JSON file to update or check.
      Defaults to npm/package.json. Intended for tests or dry runs against a copy.

  -h, --help
      Show this help text.

Examples:
  node scripts/prepare-taoeffect-release.mjs 0.10.0-taoeffect.1
  node scripts/prepare-taoeffect-release.mjs --check 0.10.0-taoeffect.1

Release tag to create after committing the package version:
  v0.10.0-taoeffect.1`;
}

function parseArgs(argv) {
  const args = {
    check: false,
    packageJSONPath: join(repoRoot, "npm", "package.json"),
    version: undefined,
  };

  for (let index = 0; index < argv.length; index++) {
    const arg = argv[index];
    if (args.version && arg.startsWith("--")) {
      throw new Error(`Unexpected option after <npm-version>: ${arg}. Options must come before the version.`);
    }
    if (arg === "--help" || arg === "-h") {
      args.help = true;
      continue;
    }
    if (arg === "--check") {
      args.check = true;
      continue;
    }
    if (arg.startsWith("--package-json=")) {
      args.packageJSONPath = arg.slice("--package-json=".length);
      continue;
    }
    if (arg === "--package-json") {
      args.packageJSONPath = argv[++index];
      if (!args.packageJSONPath || args.packageJSONPath.startsWith("--")) {
        throw new Error("Missing value for --package-json");
      }
      continue;
    }
    if (arg.startsWith("--")) {
      throw new Error(`Unknown option: ${arg}`);
    }
    if (args.version) {
      throw new Error(`Unexpected positional argument: ${arg}. Only one npm version may be provided.`);
    }
    args.version = arg;
  }

  return args;
}

function normalizeVersion(version) {
  if (!version) {
    throw new Error("Missing release version. Pass <npm-version> as the first argument.");
  }
  if (version.startsWith("v")) {
    throw new Error(`Invalid npm version: ${version}. Omit the leading "v"; the Git tag will be v${version.slice(1)}.`);
  }
  const match = version.match(semverPattern);
  if (!match) {
    throw new Error(`Invalid npm SemVer version: ${version}`);
  }
  if (!/^taoeffect\.[1-9]\d*$/.test(match[4] ?? "")) {
    throw new Error(`Fork releases must use a prerelease like 0.10.0-taoeffect.1: ${version}`);
  }
  return version;
}

async function readPackageJSON(packageJSONPath) {
  try {
    return JSON.parse(await readFile(packageJSONPath, "utf8"));
  } catch (error) {
    throw new Error(`Failed to read ${packageJSONPath}: ${error.message}`);
  }
}

function printNextSteps({ check, packageJSONPath, version }) {
  const tag = `v${version}`;
  console.log(`Version: ${version}`);
  console.log(`Tag: ${tag}`);
  console.log(`Package: ${packageJSONPath}`);
  if (!check) {
    console.log("Next steps:");
    console.log(`  git add npm/package.json scripts/prepare-taoeffect-release.mjs scripts/generate-npm-package.mjs`);
    console.log(`  git commit -m "chore(release): prepare ${tag}"`);
    console.log(`  git tag -a ${tag} -m ${tag}`);
    console.log("  git push taoeffect taoeffect");
    console.log(`  git push taoeffect ${tag}`);
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    console.log(usage());
    return;
  }

  const version = normalizeVersion(args.version);
  const packageJSONPath = resolve(args.packageJSONPath);
  const packageJSON = await readPackageJSON(packageJSONPath);

  if (args.check) {
    if (packageJSON.version !== version) {
      throw new Error(`${packageJSONPath} has version ${packageJSON.version}, expected ${version}`);
    }
    console.log(`${packageJSON.name ?? "npm package"}@${version} is prepared.`);
    printNextSteps({ check: true, packageJSONPath, version });
    return;
  }

  packageJSON.version = version;
  await writeFile(packageJSONPath, `${JSON.stringify(packageJSON, null, 2)}\n`);
  console.log(`Updated ${packageJSON.name ?? "npm package"} to ${version}.`);
  printNextSteps({ check: false, packageJSONPath, version });
}

main().catch((error) => {
  console.error(`Failed to prepare taoeffect release: ${error.message}`);
  process.exit(1);
});
