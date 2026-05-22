#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// Usage:
//   node scripts/bump-version-and-release.mjs
//
// This script automates a Tao Effect fork release after your code changes have
// already been committed on the local `taoeffect` branch. It reads the current
// npm package version from npm/package.json and compares it with the newest
// upstream-style Git tag available locally, such as `v0.71.0`.
//
// Version behavior:
//   - If a newer upstream tag exists, `0.70.0-taoeffect.2` becomes
//     `0.71.0-taoeffect.1`.
//   - If no newer upstream tag exists, it bumps the fork iteration, such as
//     `0.70.0-taoeffect.1` to `0.70.0-taoeffect.2`.
//
// It then creates the release version commit, tags it as `v<version>`, and
// pushes both the branch and tag to the `taoeffect` remote. Pushing the tag
// triggers the GitHub release workflow, which publishes the npm package through
// trusted publishing.
//
// Safety checks:
//   - The working tree must be clean before the script runs.
//   - The current branch must be `taoeffect` unless overridden.
//   - The next tag must not already exist locally.
//
// Helpful dry run:
//   node scripts/bump-version-and-release.mjs --dry-run

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const defaultPackageJSONPath = join(repoRoot, "npm", "package.json");
const prepareScriptPath = join(repoRoot, "scripts", "prepare-taoeffect-release.mjs");
const forkVersionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-taoeffect\.([1-9]\d*)$/;
const upstreamVersionPattern = /^(?:v)?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;

function usage() {
  return `Usage:
  node scripts/bump-version-and-release.mjs [options]

Options:
  --dry-run
      Print the next version, tag, and release commands without modifying files,
      committing, tagging, or pushing.

  --remote <name>
      Git remote to push the release branch and tag to.
      Defaults to taoeffect.

  --branch <name>
      Local branch that must be checked out and pushed.
      Defaults to taoeffect.

  --package-json <path>
      Package JSON file to read. Only allowed with --dry-run.
      Defaults to npm/package.json.

  --upstream-version <version>
      Override latest upstream tag detection. Only allowed with --dry-run.
      Accepts versions like 0.71.0 or v0.71.0.

  -h, --help
      Show this help text.

Default release flow:
  1. Read npm/package.json version, such as 0.70.0-taoeffect.1
  2. Read the newest local upstream tag, such as v0.71.0
  3. Use 0.71.0-taoeffect.1 if v0.71.0 is newer than the package base
  4. Otherwise bump 0.70.0-taoeffect.1 to 0.70.0-taoeffect.2
  5. Update npm/package.json via prepare-taoeffect-release.mjs
  6. Commit npm/package.json
  7. Tag the commit as v<next-version>
  8. Push branch taoeffect and tag v<next-version>`;
}

function parseArgs(argv) {
  const args = {
    branch: "taoeffect",
    dryRun: false,
    packageJSONPath: defaultPackageJSONPath,
    remote: "taoeffect",
    upstreamVersion: undefined,
  };

  for (let index = 0; index < argv.length; index++) {
    const arg = argv[index];
    if (arg === "--help" || arg === "-h") {
      args.help = true;
      continue;
    }
    if (arg === "--dry-run") {
      args.dryRun = true;
      continue;
    }
    if (arg.startsWith("--remote=")) {
      args.remote = arg.slice("--remote=".length);
      continue;
    }
    if (arg === "--remote") {
      args.remote = argv[++index];
      if (!args.remote || args.remote.startsWith("--")) {
        throw new Error("Missing value for --remote");
      }
      continue;
    }
    if (arg.startsWith("--branch=")) {
      args.branch = arg.slice("--branch=".length);
      continue;
    }
    if (arg === "--branch") {
      args.branch = argv[++index];
      if (!args.branch || args.branch.startsWith("--")) {
        throw new Error("Missing value for --branch");
      }
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
    if (arg.startsWith("--upstream-version=")) {
      args.upstreamVersion = arg.slice("--upstream-version=".length);
      continue;
    }
    if (arg === "--upstream-version") {
      args.upstreamVersion = argv[++index];
      if (!args.upstreamVersion || args.upstreamVersion.startsWith("--")) {
        throw new Error("Missing value for --upstream-version");
      }
      continue;
    }
    throw new Error(`Unknown argument: ${arg}`);
  }

  return args;
}

function run(command, args, options = {}) {
  return execFileSync(command, args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: options.capture ? ["ignore", "pipe", "pipe"] : "inherit",
  });
}

function parseUpstreamVersion(version) {
  const match = version.match(upstreamVersionPattern);
  if (!match) {
    throw new Error(`Expected upstream version like 0.71.0 or v0.71.0, got ${version}`);
  }
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    version: `${match[1]}.${match[2]}.${match[3]}`,
  };
}

function parseForkVersion(version) {
  const match = version.match(forkVersionPattern);
  if (!match) {
    throw new Error(`Expected npm/package.json version like 0.70.0-taoeffect.1, got ${version}`);
  }

  const iteration = Number(match[4]);
  if (!Number.isSafeInteger(iteration)) {
    throw new Error(`Fork release iteration is too large: ${match[4]}`);
  }

  return {
    base: parseUpstreamVersion(`${match[1]}.${match[2]}.${match[3]}`),
    iteration,
  };
}

function compareUpstreamVersions(left, right) {
  for (const key of ["major", "minor", "patch"]) {
    if (left[key] > right[key]) {
      return 1;
    }
    if (left[key] < right[key]) {
      return -1;
    }
  }
  return 0;
}

function latestLocalUpstreamVersion() {
  const tags = run("git", ["tag", "--list", "v*.*.*", "--sort=-v:refname"], { capture: true })
    .split(/\r?\n/)
    .map((tag) => tag.trim())
    .filter(Boolean);

  for (const tag of tags) {
    if (upstreamVersionPattern.test(tag)) {
      return parseUpstreamVersion(tag);
    }
  }

  throw new Error("No local upstream version tag found. Fetch upstream tags first.");
}

function nextForkVersion(currentVersion, latestUpstreamVersion) {
  const current = parseForkVersion(currentVersion);
  if (compareUpstreamVersions(latestUpstreamVersion, current.base) > 0) {
    return {
      reason: `new upstream base ${latestUpstreamVersion.version}`,
      version: `${latestUpstreamVersion.version}-taoeffect.1`,
    };
  }

  return {
    reason: `next fork iteration for upstream base ${current.base.version}`,
    version: `${current.base.version}-taoeffect.${current.iteration + 1}`,
  };
}

function commandLine(command, args) {
  return [command, ...args.map((arg) => (/[\s"'\\$]/.test(arg) ? JSON.stringify(arg) : arg))].join(" ");
}

function printPlannedCommands({ branch, packageJSONPath, remote, tag, version }) {
  const commands = [
    ["node", ["scripts/prepare-taoeffect-release.mjs", "--package-json", packageJSONPath, version]],
    ["git", ["add", "npm/package.json"]],
    ["git", ["commit", "-m", `chore(release): prepare ${tag}`]],
    ["git", ["tag", "-a", tag, "-m", tag]],
    ["git", ["push", remote, branch]],
    ["git", ["push", remote, tag]],
  ];

  console.log("Planned release commands:");
  for (const [command, args] of commands) {
    console.log(`  ${commandLine(command, args)}`);
  }
}

function assertCleanWorkingTree() {
  const status = run("git", ["status", "--porcelain"], { capture: true }).trim();
  if (status) {
    throw new Error("Working tree is not clean. Commit or stash your changes before creating a release.");
  }
}

function assertCurrentBranch(expectedBranch) {
  const currentBranch = run("git", ["branch", "--show-current"], { capture: true }).trim();
  if (currentBranch !== expectedBranch) {
    throw new Error(`Expected current branch ${expectedBranch}, got ${currentBranch || "detached HEAD"}`);
  }
}

function assertLocalTagDoesNotExist(tag) {
  try {
    run("git", ["rev-parse", "--verify", `refs/tags/${tag}`], { capture: true });
  } catch {
    return;
  }
  throw new Error(`Local tag already exists: ${tag}`);
}

async function readPackageVersion(packageJSONPath) {
  const packageJSON = JSON.parse(await readFile(packageJSONPath, "utf8"));
  if (!packageJSON.version) {
    throw new Error(`${packageJSONPath} does not contain a version field`);
  }
  return packageJSON.version;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    console.log(usage());
    return;
  }

  const packageJSONPath = resolve(args.packageJSONPath);
  if (!args.dryRun && packageJSONPath !== defaultPackageJSONPath) {
    throw new Error("--package-json is only allowed with --dry-run");
  }
  if (!args.dryRun && args.upstreamVersion) {
    throw new Error("--upstream-version is only allowed with --dry-run");
  }

  const currentVersion = await readPackageVersion(packageJSONPath);
  const upstreamVersion = args.upstreamVersion ? parseUpstreamVersion(args.upstreamVersion) : latestLocalUpstreamVersion();
  const nextVersion = nextForkVersion(currentVersion, upstreamVersion);
  const tag = `v${nextVersion.version}`;

  console.log(`Current package version: ${currentVersion}`);
  console.log(`Latest upstream version: ${upstreamVersion.version}`);
  console.log(`Next release version: ${nextVersion.version}`);
  console.log(`Reason: ${nextVersion.reason}`);
  console.log(`Tag: ${tag}`);

  if (args.dryRun) {
    printPlannedCommands({
      branch: args.branch,
      packageJSONPath,
      remote: args.remote,
      tag,
      version: nextVersion.version,
    });
    return;
  }

  assertCleanWorkingTree();
  assertCurrentBranch(args.branch);
  assertLocalTagDoesNotExist(tag);

  run("node", [prepareScriptPath, nextVersion.version]);
  run("git", ["diff", "--", "npm/package.json"]);
  run("git", ["add", "npm/package.json"]);
  run("git", ["commit", "-m", `chore(release): prepare ${tag}`]);
  run("git", ["tag", "-a", tag, "-m", tag]);
  run("git", ["push", args.remote, args.branch]);
  run("git", ["push", args.remote, tag]);

  console.log(`Released ${tag}. GitHub Actions will build and publish the npm package.`);
}

main().catch((error) => {
  console.error(`Failed to bump version and release: ${error.message}`);
  process.exit(1);
});
