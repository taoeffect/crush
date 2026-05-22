#!/usr/bin/env node
import { spawn } from "node:child_process";
import { stat } from "node:fs/promises";

import { binaryPath } from "./lib.js";

async function run() {
  try {
    await stat(binaryPath);
  } catch {
    console.error(`Crush binary is not installed at ${binaryPath}. Try reinstalling @taoeffects/crush.`);
    process.exit(1);
  }

  const child = spawn(binaryPath, process.argv.slice(2), {
    stdio: "inherit",
  });

  child.on("error", (error) => {
    console.error(`Failed to run Crush: ${error.message}`);
    process.exit(1);
  });

  child.on("exit", (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code ?? 0);
  });
}

run().catch((error) => {
  console.error(`Failed to run Crush: ${error.message}`);
  process.exit(1);
});
