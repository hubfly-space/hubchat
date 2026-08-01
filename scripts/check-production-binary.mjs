import { spawn } from "node:child_process";
import net from "node:net";

const binary = process.env.HUBCHAT_BINARY_PATH ?? "./dist/hubchat";
const databaseURL = process.env.HUBCHAT_BINARY_DATABASE_URL ?? process.env.HUBCHAT_TEST_DATABASE_URL;
const secretKey = process.env.HUBCHAT_BINARY_SECRET_KEY ?? "release-smoke-only-secret-key-32-bytes!!";

if (!databaseURL) {
  console.error("HUBCHAT_BINARY_DATABASE_URL or HUBCHAT_TEST_DATABASE_URL is required");
  process.exit(1);
}

function reservePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") {
        server.close();
        reject(new Error("could not determine an ephemeral port"));
        return;
      }
      const { port } = address;
      server.close((error) => (error ? reject(error) : resolve(port)));
    });
  });
}

function waitForExit(child) {
  return new Promise((resolve) => child.once("exit", (code, signal) => resolve({ code, signal })));
}

async function checkBackgroundRoles(env) {
  const output = [];
  const child = spawn(binary, ["serve", "--roles=worker,scheduler"], {
    env,
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout.on("data", (chunk) => output.push(String(chunk)));
  child.stderr.on("data", (chunk) => output.push(String(chunk)));

  const exitPromise = waitForExit(child);
  let stopped = false;
  try {
    const started = await Promise.race([
      new Promise((resolve) => setTimeout(() => resolve(true), 1000)),
      exitPromise.then((result) => {
        throw new Error(`worker/scheduler roles exited before probe: ${result.signal ?? `code ${result.code}`}\n${output.join("").slice(-4000)}`);
      }),
    ]);
    if (!started) {
      throw new Error("worker/scheduler role probe did not start");
    }

    child.kill("SIGTERM");
    stopped = true;
    const result = await Promise.race([
      exitPromise,
      new Promise((_, reject) => setTimeout(() => reject(new Error(`worker/scheduler roles did not shut down within 10 seconds\n${output.join("").slice(-4000)}`)), 10_000)),
    ]);
    if (result.code !== 0) {
      throw new Error(`worker/scheduler roles exited with ${result.signal ?? `code ${result.code}`}\n${output.join("").slice(-4000)}`);
    }
  } finally {
    if (!stopped) child.kill("SIGTERM");
  }
}

function runNodeScript(script, env) {
  return new Promise((resolve, reject) => {
    const processHandle = spawn(process.execPath, [script], { env, stdio: "inherit" });
    processHandle.once("error", reject);
    processHandle.once("exit", (code, signal) => {
      if (code === 0) resolve();
      else reject(new Error(`${script} exited with ${signal ?? `code ${code}`}`));
    });
  });
}

const port = await reservePort();
const baseURL = `http://127.0.0.1:${port}`;
const output = [];
const child = spawn(binary, ["serve", "--roles=http,realtime,worker,scheduler"], {
  env: {
    ...process.env,
    HUBCHAT_DATABASE_URL: databaseURL,
    HUBCHAT_PUBLIC_URL: baseURL,
    HUBCHAT_LISTEN: `127.0.0.1:${port}`,
    HUBCHAT_SECRET_KEY: secretKey,
    HUBCHAT_SLA_EVALUATION_INTERVAL: "1s",
    HUBCHAT_DEV: "0",
    HUBCHAT_MIGRATE: "verify",
  },
  stdio: ["ignore", "pipe", "pipe"],
});
child.stdout.on("data", (chunk) => output.push(String(chunk)));
child.stderr.on("data", (chunk) => output.push(String(chunk)));

let stopped = false;
try {
  const deadline = Date.now() + 15_000;
  let ready = false;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${baseURL}/healthz`, { signal: AbortSignal.timeout(500) });
      if (response.status === 200) {
        ready = true;
        break;
      }
    } catch {
      // The process may still be loading configuration or binding its port.
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  if (!ready) {
    throw new Error(`production binary did not become ready\n${output.join("").slice(-4000)}`);
  }

  const childEnv = {
    ...process.env,
    HUBCHAT_SMOKE_BASE_URL: baseURL,
    HUBCHAT_LOAD_BASE_URL: baseURL,
    HUBCHAT_LOAD_DURATION_MS: process.env.HUBCHAT_LOAD_DURATION_MS ?? "3000",
    HUBCHAT_LOAD_CONCURRENCY: process.env.HUBCHAT_LOAD_CONCURRENCY ?? "16",
  };
  await runNodeScript("scripts/check-production-http.mjs", childEnv);
  const journeyEnv = {
    ...childEnv,
    HUBCHAT_JOURNEY_BASE_URL: baseURL,
  };
  if (process.env.HUBCHAT_RUN_BROWSER_CHECK === "1") journeyEnv.HUBCHAT_JOURNEY_BROWSER = "1";
  await runNodeScript("scripts/check-production-journey.mjs", journeyEnv);
  await runNodeScript("scripts/check-production-load.mjs", childEnv);

  const exitPromise = waitForExit(child);
  child.kill("SIGTERM");
  stopped = true;
  const result = await Promise.race([
    exitPromise,
    new Promise((_, reject) => setTimeout(() => reject(new Error("production binary did not shut down within 10 seconds")), 10_000)),
  ]);
  if (result.code !== 0) {
    throw new Error(`production binary exited with ${result.signal ?? `code ${result.code}`}`);
  }
  await checkBackgroundRoles({
    ...process.env,
    HUBCHAT_DATABASE_URL: databaseURL,
    HUBCHAT_PUBLIC_URL: baseURL,
    HUBCHAT_SECRET_KEY: secretKey,
    HUBCHAT_DEV: "0",
    HUBCHAT_MIGRATE: "verify",
  });
  console.log("Production binary release check OK (build, readiness, smoke, load, HTTP shutdown, worker/scheduler shutdown)");
} finally {
  if (!stopped) child.kill("SIGTERM");
}
