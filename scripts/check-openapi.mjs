import { readFileSync } from "node:fs";

const path = "embedded/openapi.json";
const document = JSON.parse(readFileSync(path, "utf8"));
const methods = new Set(["get", "post", "put", "patch", "delete", "options", "head", "trace"]);
const errors = [];
const operationIDs = new Set();
const parameterByRef = document.components?.parameters ?? {};

function parameterDetails(parameter) {
  if (parameter?.$ref?.startsWith("#/components/parameters/")) {
    return parameterByRef[parameter.$ref.slice("#/components/parameters/".length)] ?? {};
  }
  return parameter ?? {};
}

if (document.openapi !== "3.1.0") errors.push("openapi.json must use OpenAPI 3.1.0");
if (!document.info?.title || !document.info?.version) errors.push("info.title and info.version are required");
if (!document.components?.securitySchemes?.bearerAuth) errors.push("bearerAuth security scheme is required");
if (document.components?.operations) errors.push("operation templates must be expanded before publishing");

for (const [route, pathItem] of Object.entries(document.paths ?? {})) {
  if (!route.startsWith("/v1/")) errors.push(`${route}: public paths must be versioned under /v1/`);
  const pathParameters = new Set(
    (pathItem.parameters ?? [])
      .map(parameterDetails)
      .filter((parameter) => parameter.in === "path")
      .map((parameter) => parameter.name),
  );

  for (const [method, operation] of Object.entries(pathItem)) {
    if (!methods.has(method)) continue;
    if (!operation.operationId) {
      errors.push(`${method.toUpperCase()} ${route}: operationId is required`);
    } else if (operationIDs.has(operation.operationId)) {
      errors.push(`${method.toUpperCase()} ${route}: duplicate operationId ${operation.operationId}`);
    } else {
      operationIDs.add(operation.operationId);
    }
    if (!operation.responses || Object.keys(operation.responses).length === 0) {
      errors.push(`${method.toUpperCase()} ${route}: responses are required`);
    }

    const operationParameters = new Set(
      (operation.parameters ?? [])
        .map(parameterDetails)
        .filter((parameter) => parameter.in === "path")
        .map((parameter) => parameter.name),
    );
    for (const name of route.matchAll(/\{([^}]+)\}/g)) {
      if (!pathParameters.has(name[1]) && !operationParameters.has(name[1])) {
        errors.push(`${method.toUpperCase()} ${route}: missing path parameter ${name[1]}`);
      }
    }
  }
}

if (errors.length > 0) {
  console.error(errors.map((error) => `- ${error}`).join("\n"));
  process.exit(1);
}

console.log(`OpenAPI contract OK (${Object.keys(document.paths ?? {}).length} paths, ${operationIDs.size} operations)`);
