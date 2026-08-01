import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const templatePath = "embedded/openapi.template.json";
const outputPath = "embedded/openapi.json";
const document = JSON.parse(readFileSync(templatePath, "utf8"));
const operationTemplates = document.components?.operations ?? {};
const operationSchemas = document.components?.operationSchemas ?? {};

// Keep the published contract from silently falling behind the Go mux. The
// hand-authored template remains authoritative for rich schemas and examples;
// newly registered routes receive a conservative baseline operation until a
// richer description is added. This makes every reachable public operation
// discoverable without pretending that an undocumented request body has a
// precise schema.
const methods = new Set(["GET", "POST", "PUT", "PATCH", "DELETE"]);
const registeredRoutes = new Map();
for (const file of readdirSync("internal/api").filter((name) => name.endsWith(".go"))) {
  const source = readFileSync(join("internal/api", file), "utf8");
  for (const match of source.matchAll(/HandleFunc\("(GET|POST|PUT|PATCH|DELETE) ([^" ]+)/g)) {
    const [, method, route] = match;
    if (!route.startsWith("/v1/")) continue;
    registeredRoutes.set(`${method} ${route}`, { method: method.toLowerCase(), route });
  }
}

function operationID(method, route) {
  const parts = route
    .replace(/^\/v1\//, "")
    .split("/")
    .flatMap((part) => part.replace(/[{}]/g, "").split(/[^A-Za-z0-9]+/))
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1));
  return method.toLowerCase() + parts.join("");
}

function isPublicRoute(route) {
  return route.startsWith("/v1/public/") || route.startsWith("/v1/widget/") ||
    route.startsWith("/v1/portal/") || route === "/v1/openapi.json" ||
    route.startsWith("/v1/auth/") || route === "/v1/auth/login" ||
    route === "/v1/auth/signup";
}

function baselineOperation(method, route) {
  const parameters = [...route.matchAll(/\{([^}]+)\}/g)].map((match) => ({
    name: match[1],
    in: "path",
    required: true,
    schema: { type: "string" },
  }));
  const operation = {
    operationId: operationID(method, route),
    summary: `${method} ${route}`,
    description: "This endpoint is implemented by the Hubchat API. Request and response details are versioned with the running service.",
    parameters,
    responses: {
      "200": {
        description: "Successful response.",
        content: { "application/json": { schema: { type: "object", additionalProperties: true } } },
      },
      "400": { description: "The request is invalid." },
      "401": { description: "Authentication is required." },
      "403": { description: "The actor lacks the required capability." },
      "404": { description: "The resource was not found." },
    },
  };
  if (!isPublicRoute(route)) operation.security = [{ bearerAuth: [] }];
  if (method !== "get" && method !== "delete") {
    operation.requestBody = {
      required: false,
      content: { "application/json": { schema: { type: "object", additionalProperties: true } } },
    };
  }
  return operation;
}

for (const pathItem of Object.values(document.paths ?? {})) {
  for (const method of ["get", "post", "put", "patch", "delete", "options", "head", "trace"]) {
    const operation = pathItem[method];
    if (!operation || typeof operation !== "object" || typeof operation.$ref !== "string") {
      continue;
    }

    const prefix = "#/components/operations/";
    if (!operation.$ref.startsWith(prefix)) {
      throw new Error(`Unsupported operation reference: ${operation.$ref}`);
    }
    const templateName = operation.$ref.slice(prefix.length);
    const template = operationTemplates[templateName];
    if (!template) {
      throw new Error(`Missing operation template: ${templateName}`);
    }

    const { $ref: ignored, ...overrides } = operation;
    // Templates contain nested response/parameter objects. Clone before
    // applying path-specific schema bindings so every operation gets its own
    // response tree instead of sharing the last list/create override.
    pathItem[method] = { ...structuredClone(template), ...structuredClone(overrides) };
  }
}

for (const { method, route } of registeredRoutes.values()) {
  const pathItem = document.paths[route] ?? (document.paths[route] = {});
  if (!pathItem[method] && methods.has(method.toUpperCase())) {
    pathItem[method] = baselineOperation(method, route);
  }
}

// Operation-to-schema bindings are template metadata. Applying them here
// keeps the published document standard OpenAPI while letting route discovery
// continue to provide a safe baseline for new handlers.
function schemaRef(value, context) {
  if (typeof value === "string") return { $ref: `#/components/schemas/${value}` };
  if (value && typeof value === "object" && value.$ref) return value;
  throw new Error(`${context}: schema name or $ref is required`);
}

function applyOperationSchema(operation, operationID, binding) {
  if (!binding || typeof binding !== "object") return;
  if (binding.request) {
    operation.requestBody = {
      required: binding.requestRequired !== false,
      content: { "application/json": { schema: schemaRef(binding.request, `${operationID}.request`) } },
    };
  }
  for (const [status, schema] of Object.entries(binding.responses ?? {})) {
    if (!operation.responses?.[status]) {
      throw new Error(`${operationID}: response ${status} is not present in the operation`);
    }
    const response = operation.responses[status];
    operation.responses[status] = {
      description: response.description ?? "Successful JSON response.",
      content: { "application/json": { schema: schemaRef(schema, `${operationID}.responses.${status}`) } },
    };
  }
}

for (const pathItem of Object.values(document.paths ?? {})) {
  for (const method of methods) {
    const operation = pathItem[method.toLowerCase()];
    if (!operation || !operation.operationId) continue;
    applyOperationSchema(operation, operation.operationId, operationSchemas[operation.operationId]);
  }
}

if (document.components) {
  delete document.components.operations;
  delete document.components.operationSchemas;
}

writeFileSync(outputPath, `${JSON.stringify(document, null, 2)}\n`);
