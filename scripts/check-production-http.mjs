const baseURL = process.env.HUBCHAT_SMOKE_BASE_URL;

if (!baseURL) {
  console.error("HUBCHAT_SMOKE_BASE_URL is required, for example http://127.0.0.1:18080");
  process.exit(1);
}

const checks = [
  { path: "/healthz", status: 200, body: '"status":"ok"' },
  { path: "/readyz", status: 200, body: '"status":"ok"' },
  { path: "/app/", status: 200, body: "<html" },
  { path: "/portal/", status: 200, body: "<html" },
  { path: "/widget/app.js", status: 200, body: "Hubchat", contentType: "javascript" },
  { path: "/api/v1/meta", status: 200, body: '"surface":"api"' },
];

for (const check of checks) {
  const response = await fetch(new URL(check.path, baseURL));
  const body = await response.text();
  if (response.status !== check.status) {
    throw new Error(`${check.path}: status ${response.status}, expected ${check.status}`);
  }
  if (!body.includes(check.body)) {
    throw new Error(`${check.path}: response did not contain ${JSON.stringify(check.body)}`);
  }
  if (check.contentType && !response.headers.get("content-type")?.includes(check.contentType)) {
    throw new Error(`${check.path}: unexpected content type ${response.headers.get("content-type")}`);
  }
  console.log(`OK ${check.path} (${response.status})`);
}
