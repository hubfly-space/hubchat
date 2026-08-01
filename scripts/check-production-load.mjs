const baseURL = process.env.HUBCHAT_LOAD_BASE_URL;

if (!baseURL) {
  console.error("HUBCHAT_LOAD_BASE_URL is required, for example http://127.0.0.1:18080");
  process.exit(1);
}

function boundedInteger(name, fallback, minimum, maximum) {
  const raw = process.env[name];
  if (raw === undefined || raw === "") return fallback;
  const value = Number.parseInt(raw, 10);
  if (!Number.isInteger(value) || value < minimum || value > maximum) {
    throw new Error(`${name} must be an integer between ${minimum} and ${maximum}`);
  }
  return value;
}

const durationMs = boundedInteger("HUBCHAT_LOAD_DURATION_MS", 5_000, 500, 120_000);
const concurrency = boundedInteger("HUBCHAT_LOAD_CONCURRENCY", 16, 1, 256);
const timeoutMs = boundedInteger("HUBCHAT_LOAD_TIMEOUT_MS", 2_000, 100, 30_000);

const endpoints = [
  { path: "/healthz", allowedStatuses: [200] },
  { path: "/readyz", allowedStatuses: [200] },
  { path: "/app/", allowedStatuses: [200] },
  { path: "/portal/", allowedStatuses: [200] },
  { path: "/widget/app.js", allowedStatuses: [200] },
  // The API surface is rate limited by design. A 429 here proves the
  // protection engaged under load; health, readiness, and static assets must
  // still remain available.
  { path: "/api/v1/meta", allowedStatuses: [200, 429] },
];

const stats = {
  completed: 0,
  failed: 0,
  statuses: new Map(),
  latencies: [],
  errors: [],
};
let nextEndpoint = 0;
const deadline = Date.now() + durationMs;

async function request(endpoint) {
  const started = performance.now();
  try {
    const response = await fetch(new URL(endpoint.path, baseURL), {
      signal: AbortSignal.timeout(timeoutMs),
    });
    await response.arrayBuffer();
    const latency = performance.now() - started;
    stats.completed += 1;
    stats.latencies.push(latency);
    stats.statuses.set(response.status, (stats.statuses.get(response.status) ?? 0) + 1);
    if (!endpoint.allowedStatuses.includes(response.status)) {
      stats.failed += 1;
      if (stats.errors.length < 10) {
        stats.errors.push(`${endpoint.path}: status ${response.status}, expected one of ${endpoint.allowedStatuses.join(", ")}`);
      }
    }
  } catch (error) {
    stats.failed += 1;
    if (stats.errors.length < 10) {
      stats.errors.push(`${endpoint.path}: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
}

async function worker() {
  while (Date.now() < deadline) {
    const endpoint = endpoints[nextEndpoint % endpoints.length];
    nextEndpoint += 1;
    await request(endpoint);
  }
}

await Promise.all(Array.from({ length: concurrency }, worker));

stats.latencies.sort((a, b) => a - b);
const percentile = (fraction) => {
  if (stats.latencies.length === 0) return 0;
  return stats.latencies[Math.min(stats.latencies.length - 1, Math.ceil(stats.latencies.length * fraction) - 1)];
};
const statusSummary = [...stats.statuses.entries()]
  .sort(([left], [right]) => left - right)
  .map(([status, count]) => `${status}:${count}`)
  .join(", ");

console.log(`Production load smoke: ${stats.completed} requests, ${concurrency} workers, ${durationMs}ms`);
console.log(`Statuses: ${statusSummary || "none"}`);
console.log(`Latency ms: p50=${percentile(0.50).toFixed(1)} p95=${percentile(0.95).toFixed(1)} p99=${percentile(0.99).toFixed(1)}`);

if (stats.failed > 0 || stats.completed === 0) {
  for (const error of stats.errors) console.error(`- ${error}`);
  process.exit(1);
}
