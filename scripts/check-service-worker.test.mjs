import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { test } from "node:test";
import assert from "node:assert/strict";

function worker(clients = []) {
  const listeners = new Map();
  const notifications = [];
  const opened = [];
  const self = {
    location: { origin: "https://hubchat.example" },
    addEventListener: (type, listener) => listeners.set(type, listener),
    skipWaiting: () => {},
    registration: { showNotification: async (title, options) => notifications.push({ title, options }) },
    clients: { claim: async () => {}, matchAll: async () => clients, openWindow: async (url) => opened.push(url) },
  };
  runInNewContext(readFileSync(new URL("../web/dashboard/public/sw.js", import.meta.url), "utf8"), { self, URL });
  const dispatch = async (type, fields) => {
    let completion;
    listeners.get(type)({ ...fields, waitUntil: (promise) => { completion = promise; } });
    await completion;
  };
  return { dispatch, notifications, opened };
}

test("a visible tab does not discard a push intended for another workspace", async () => {
  let refreshed = false;
  const fixture = worker([{ visibilityState: "visible", postMessage: () => { refreshed = true; } }]);
  await fixture.dispatch("push", { data: { json: () => ({ title: "Hubchat", body: "New notification", url: "/app/inbox?workspace_id=other" }) } });
  assert.equal(refreshed, true);
  assert.equal(fixture.notifications.length, 1);
});

test("notification navigation cannot open an external URL", async () => {
  const fixture = worker();
  await fixture.dispatch("notificationclick", { notification: { close() {}, data: { url: "https://untrusted.example/app/" } } });
  assert.equal(fixture.opened.length, 0);
  await fixture.dispatch("notificationclick", { notification: { close() {}, data: { url: "/app/inbox?workspace_id=allowed" } } });
  assert.equal(fixture.opened[0], "https://hubchat.example/app/inbox?workspace_id=allowed");
});
