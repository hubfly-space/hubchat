import { readFileSync, writeFileSync } from "node:fs";

const templatePath = "embedded/openapi.template.json";
const outputPath = "embedded/openapi.json";
const document = JSON.parse(readFileSync(templatePath, "utf8"));
const operationTemplates = document.components?.operations ?? {};

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
    pathItem[method] = { ...template, ...overrides };
  }
}

if (document.components) {
  delete document.components.operations;
}

writeFileSync(outputPath, `${JSON.stringify(document, null, 2)}\n`);
