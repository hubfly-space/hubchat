import { readFileSync } from "node:fs";

const loader = readFileSync("web/widget/public/v1.js", "utf8");
const adapter = readFileSync("web/widget/src/main.tsx", "utf8");
const widget = readFileSync("web/widget/src/panel/Widget.tsx", "utf8");
const declarations = readFileSync("web/widget/public/v1.d.ts", "utf8");
const docs = readFileSync("docs/widget-sdk.md", "utf8");

const commands = [
  "boot", "show", "open", "hide", "close", "toggle", "identify", "context",
  "update", "track", "startConversation", "openArticle", "openForm",
  "openTicketForm", "openFeedback", "openFeedbackForm", "reset", "on",
];
const events = [
  "ready", "open", "close", "message:received", "conversation:started", "unread:changed",
];
const errors = [];

const commandSet = adapter.match(/const commands = new Set\(\[([\s\S]*?)\]\);/)?.[1] ?? "";
for (const command of commands) {
  const implemented = command === "boot"
    ? loader.includes('method === "boot"')
    : commandSet.includes(`"${command}"`);
  if (!implemented) errors.push(`loader is missing command: ${command}`);
  if (!declarations.includes(`"${command}"`)) errors.push(`declarations are missing command: ${command}`);
  if (!docs.includes(`Hubchat("${command}"`)) errors.push(`docs are missing command example: ${command}`);
}

for (const event of events) {
  if (!widget.includes(`onEvent("${event}"`)) errors.push(`widget is missing lifecycle event: ${event}`);
  if (!declarations.includes(`"${event}"`)) errors.push(`declarations are missing lifecycle event: ${event}`);
  if (!docs.includes(`\`${event}\``) && !docs.includes(`event: "${event}"`)) {
    errors.push(`docs are missing lifecycle event: ${event}`);
  }
}

if (errors.length) {
  console.error(errors.map((error) => `- ${error}`).join("\n"));
  process.exit(1);
}

console.log(`Widget SDK contract OK (${commands.length} commands, ${events.length} lifecycle events)`);
