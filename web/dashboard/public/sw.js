/* Push-only service worker: authenticated API responses are never cached. */
self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));
self.addEventListener("push", (event) => {
  event.waitUntil((async () => {
    let payload = {};
    try { payload = event.data?.json() ?? {}; } catch { /* show a generic alert */ }
    const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const client of clients) client.postMessage({ type: "hubchat:notification" });
    // A visible tab may belong to a different workspace or signed-in member.
    // Never drop delivery based only on another tab's visibility. The client
    // suppresses its own desktop alert when this member has opted into push.
    await self.registration.showNotification(payload.title || "Hubchat", {
      body: payload.body || "You have a new notification.", icon: "/app/favicon.svg",
      tag: payload.tag, data: { url: payload.url || "/app/inbox" },
    });
  })());
});
self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil((async () => {
    const target = new URL(event.notification.data?.url || "/app/inbox", self.location.origin);
    if (target.origin !== self.location.origin || !target.pathname.startsWith("/app/")) return;
    const clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    const client = clients.find((item) => new URL(item.url).pathname.startsWith("/app/"));
    if (client) { await client.navigate(target.href); await client.focus(); }
    else await self.clients.openWindow(target.href);
  })());
});
