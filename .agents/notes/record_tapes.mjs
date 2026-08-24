#!/usr/bin/env node
import { readdirSync, rmSync } from "node:fs";
import { basename, extname, join } from "node:path";
import { spawnSync } from "node:child_process";

const cassetteDir = "internal/agent/testdata/TestCoderAgent/deepseek-v4";
const packagePath = "./internal/agent";
const modelSubtest = "deepseek-v4";

function discoverTests() {
  const result = spawnSync("git", ["ls-files", `${cassetteDir}/*.yaml`], {
    encoding: "utf8",
  });
  if (result.status === 0 && result.stdout.trim() !== "") {
    return [...new Set(result.stdout
      .trim()
      .split(/\r?\n/)
      .map((name) => basename(name, ".yaml")))]
      .sort();
  }

  return readdirSync(cassetteDir)
    .filter((name) => extname(name) === ".yaml")
    .map((name) => basename(name, ".yaml"))
    .sort();
}

const requestedTests = process.argv.slice(2).filter((arg) => !arg.startsWith("-"));
const tests = requestedTests.length > 0 ? requestedTests : discoverTests();
const timeoutScheduleSeconds = [60, 120, 240, 300];

function cassettePath(test) {
  return join(cassetteDir, `${test}.yaml`);
}

function timestamp() {
  return new Date().toISOString();
}

function log(message) {
  console.log(`[${timestamp()}] ${message}`);
}

function warn(message) {
  console.warn(`[${timestamp()}] ${message}`);
}

function error(message) {
  console.error(`[${timestamp()}] ${message}`);
}

function runGoTest(test, timeoutSeconds) {
  const runPattern = `^TestCoderAgent/${modelSubtest}/${test}$`;
  const args = [
    "test",
    packagePath,
    "-run",
    runPattern,
    "-count=1",
    `-timeout=${timeoutSeconds}s`,
    "-v",
  ];

  log(`== ${test} (${timeoutSeconds}s timeout) ==`);
  log(`$ rm -f ${cassettePath(test)}`);
  rmSync(cassettePath(test), { force: true });
  log(`$ timeout ${timeoutSeconds}s go ${args.join(" ")}`);

  const started = Date.now();
  const result = spawnSync("timeout", [`${timeoutSeconds}s`, "go", ...args], {
    stdio: "inherit",
    env: process.env,
  });
  const elapsedSeconds = ((Date.now() - started) / 1000).toFixed(1);

  if (result.status === 0) {
    log(`PASS ${test} (${elapsedSeconds}s)`);
    return { ok: true, elapsedSeconds };
  }

  rmSync(cassettePath(test), { force: true });
  const reason = result.error?.code === "ETIMEDOUT" || result.signal
    ? `timeout/signal ${result.signal ?? result.error?.code}`
    : `exit ${result.status}`;
  log(`DEFER ${test} (${elapsedSeconds}s, ${reason})`);
  return { ok: false, elapsedSeconds, reason };
}

function verifyReplay() {
  const args = [
    "test",
    packagePath,
    "-run",
    `^TestCoderAgent/${modelSubtest}$`,
    "-count=1",
    "-timeout=5m",
  ];
  log("== replay verification ==");
  log(`$ go ${args.join(" ")}`);
  const result = spawnSync("go", args, { stdio: "inherit", env: process.env });
  if (result.status !== 0) {
    error("Replay verification failed.");
    process.exit(result.status ?? 1);
  }
}

if (!process.env.CRUSH_HYPER_API_KEY) {
  warn("warning: CRUSH_HYPER_API_KEY is not set; recording live Hyper tapes will likely fail.");
}

log(`Recording ${tests.length} tape(s): ${tests.join(", ")}`);

let deferred = tests;
const recorded = [];
const attempts = [];

for (const timeoutSeconds of timeoutScheduleSeconds) {
  const nextDeferred = [];

  for (const test of deferred) {
    const result = runGoTest(test, timeoutSeconds);
    attempts.push({ test, timeoutSeconds, ...result });

    if (result.ok) {
      recorded.push(test);
      continue;
    }

    nextDeferred.push(test);

    if (timeoutSeconds >= 300) {
      const remaining = [...new Set([...nextDeferred, ...deferred.slice(deferred.indexOf(test) + 1)])];
      error("Aborting: these tests failed at the 5-minute timeout:");
      for (const failed of remaining) {
        error(`- ${failed}`);
      }
      process.exit(1);
    }
  }

  if (nextDeferred.length === 0) {
    log("All requested tapes recorded.");
    verifyReplay();
    process.exit(0);
  }

  log(`Deferred after ${timeoutSeconds}s pass: ${nextDeferred.join(", ")}`);
  deferred = nextDeferred;
}

error("Unexpectedly exhausted timeout schedule.");
process.exit(1);
