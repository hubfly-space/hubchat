/**
 * Explicit portal demo fixtures.
 *
 * These are used only while reviewing the portal shell and in isolated visual
 * demos. The production portal must load workspace content and customer data
 * from the API and show a real loading/error state instead of falling back to
 * these rows.
 */

export const NOW = new Date("2026-07-26T14:20:00Z");

export const portal = {
  name: "Northwind Help Centre",
  accent: "#3B6EF6",
  headline: "How can we help?",
  subheadline: "Search our guides, track your requests, or start a conversation with the team.",
  navigation: [
    { label: "Guides", href: "/kb" },
    { label: "Requests", href: "/tickets" },
    { label: "Roadmap", href: "/feedback" },
    { label: "Changelog", href: "/changelog" },
  ],
  footerLinks: [
    { label: "Status", href: "https://status.northwind.cloud" },
    { label: "Privacy", href: "https://northwind.cloud/privacy" },
    { label: "Terms", href: "https://northwind.cloud/terms" },
  ],
};

export const viewer = {
  name: "Mariana Costa",
  email: "mariana@atlasfreight.com",
  company: "Atlas Freight",
  signedIn: true,
};

export const collections = [
  { slug: "getting-started", name: "Getting started", description: "Install, configure, and send your first message.", icon: "rocket", count: 12 },
  { slug: "widget", name: "Widget & portal", description: "Everything your customers see.", icon: "layout", count: 19 },
  { slug: "api", name: "API & webhooks", description: "Integrate with your own stack.", icon: "code", count: 24 },
  { slug: "billing", name: "Billing", description: "Plans, invoices, and limits.", icon: "receipt", count: 8 },
];

export const articles = [
  {
    slug: "verifying-webhook-signatures",
    collection: "api",
    title: "Verifying webhook signatures",
    excerpt: "Check both the signature and the timestamp. The signature alone does not protect you from replay.",
    updated: "2026-07-12T10:00:00Z",
    readingMinutes: 4,
    helpful: 214,
    body: `Every webhook request carries two headers:

    Hubchat-Signature: v1=5257a869e7ecebeda32affa62cdca3fa
    Hubchat-Timestamp: 1774526400

Verify both. A valid signature on a replayed request is still a replayed request.

## Constructing the signed payload

Concatenate the timestamp, a literal dot, and the raw request body — the exact bytes you received, before any JSON parsing. Re-serialising the body will change the whitespace and the signature will not match.

## Rejecting stale requests

Reject anything whose timestamp is more than five minutes old. Compare digests in constant time; a naive string comparison leaks timing information about how much of the signature was correct.

## If verification fails

Return a 4xx. Hubchat retries with exponential backoff, and after six consecutive failures it disables the endpoint and tells the workspace why — which is much easier to debug than silence.`,
  },
  {
    slug: "installing-the-widget",
    collection: "getting-started",
    title: "Installing the widget with a script tag",
    excerpt: "One tag before the closing body element. It loads asynchronously and never blocks your page.",
    updated: "2026-06-16T09:00:00Z",
    readingMinutes: 3,
    helpful: 402,
    body: `Paste this immediately before your closing body tag.

    <script>
      Hubchat('boot', { key: 'pk_live_…' });
    </script>

## Why it does not slow your site down

The loader is a couple of kilobytes. It requests the interface only after the page has finished its own work, and the interface itself is not fetched at all until the visitor opens the launcher.

## Restricting where it appears

Add your domains to the allowlist in the workspace, then use include and exclude URL patterns to keep the widget off pages where it would be unwelcome — checkout flows, most commonly.`,
  },
  {
    slug: "signed-identity-tokens",
    collection: "api",
    title: "Identifying customers with a signed token",
    excerpt: "A customer ID from the browser is a claim. A signed token is proof.",
    updated: "2026-07-17T09:00:00Z",
    readingMinutes: 6,
    helpful: 188,
    body: `Anyone can open a browser console and claim to be anyone. That is why an unverified identity shows a warning badge to the agent, and why agents are trained not to disclose account details against one.

## Sign on your server

Generate the token server-side, with a secret the browser never sees. Include the workspace, the external customer ID, an issued-at time, an expiry, and a single-use nonce.

## What gets verified

The signature, the workspace, the expiry, the nonce against the replay window, and whether the external ID is already claimed by a different verified identity. A mismatch is refused rather than silently merged.`,
  },
  {
    slug: "domain-allowlists",
    collection: "widget",
    title: "Restricting the widget to specific domains",
    excerpt: "The widget refuses to boot on an origin you have not allowed.",
    updated: "2026-07-04T09:00:00Z",
    readingMinutes: 2,
    helpful: 96,
    body: `Add each origin that should be able to load the widget. Requests from anywhere else are rejected before any configuration is returned — an attacker cannot learn your inbox names or welcome copy by embedding your public key on their own site.

## Wildcards

Subdomain wildcards are supported. A bare wildcard is not.`,
  },
  {
    slug: "monthly-active-contacts",
    collection: "billing",
    title: "Understanding monthly active contacts",
    excerpt: "What counts, what does not, and when the counter resets.",
    updated: "2026-07-26T08:00:00Z",
    readingMinutes: 3,
    helpful: 41,
    body: `A contact is active in a month if they sent at least one message, submitted a form, or voted on feedback.

## What does not count

Browsing help articles does not make a contact active. Neither does receiving a notification. Anonymous visitors who never interact are not counted at all.`,
  },
];

export const tickets = [
  {
    number: "SUP-4471",
    title: "Webhook deliveries failing with 421 after 4.12 upgrade",
    status: "open" as const,
    updated: "2026-07-26T14:16:00Z",
    created: "2026-07-26T11:10:00Z",
    messages: [
      { id: 1, from: "you" as const, name: "Mariana Costa", at: "2026-07-26T11:10:00Z", body: "Morning — since we upgraded to 4.12 on Tuesday every webhook to our order.created endpoint is failing. We're getting nothing at all on our side." },
      { id: 2, from: "agent" as const, name: "Ada Mwangi", at: "2026-07-26T11:22:00Z", body: "Hi Mariana — thanks for flagging this so quickly. I can see the delivery failures on our side too. Before I dig in: did your endpoint URL or its TLS certificate change recently?" },
      { id: 3, from: "you" as const, name: "Mariana Costa", at: "2026-07-26T11:30:00Z", body: "No changes at all. Same URL, same cert, renewed back in April." },
      { id: 4, from: "agent" as const, name: "Ada Mwangi", at: "2026-07-26T12:20:00Z", body: "Found it — every attempt is returning 421 Misdirected Request. That points at SNI handling on your load balancer rather than the payload itself. I've attached the last ten delivery attempts so your infra team has the raw responses.", attachment: "deliveries-2026-07-24.json" },
    ],
  },
  {
    number: "SUP-4452",
    title: "Annual invoicing for 240 seats",
    status: "on_hold" as const,
    updated: "2026-07-25T12:20:00Z",
    created: "2026-07-24T22:20:00Z",
    messages: [
      { id: 1, from: "you" as const, name: "Mariana Costa", at: "2026-07-24T22:20:00Z", body: "Could we move to annual invoicing for our 240 seats? Finance would prefer a single PO." },
    ],
  },
  {
    number: "SUP-4440",
    title: "Attachment previews not rendering in Safari",
    status: "resolved" as const,
    updated: "2026-07-25T18:20:00Z",
    created: "2026-07-24T18:20:00Z",
    messages: [
      { id: 1, from: "you" as const, name: "Mariana Costa", at: "2026-07-24T18:20:00Z", body: "Image attachments show a broken icon in Safari 18." },
      { id: 2, from: "agent" as const, name: "Sara Lindqvist", at: "2026-07-25T09:02:00Z", body: "Fixed and deployed — a content-disposition header was being set on previews as well as downloads. Thanks for the clear report." },
    ],
  },
];

export const feedback = [
  { id: "fbi_1", title: "Bulk reassign conversations from the inbox", description: "Selecting 20 threads and reassigning them one at a time is painful during handover.", status: "in_progress" as const, votes: 214, comments: 18, voted: true },
  { id: "fbi_2", title: "Scheduled reports by email", description: "Send the weekly SLA summary to our ops channel automatically.", status: "planned" as const, votes: 176, comments: 9, voted: false },
  { id: "fbi_3", title: "Webhook replay from the dashboard", description: "When our endpoint is down we want to replay the window, not lose it.", status: "reviewing" as const, votes: 143, comments: 22, voted: true },
  { id: "fbi_5", title: "Official Go SDK", description: "We'd rather not hand-roll the HTTP client.", status: "open" as const, votes: 87, comments: 14, voted: false },
  { id: "fbi_4", title: "Dark mode for the customer portal", description: "Our users have asked for this repeatedly.", status: "completed" as const, votes: 98, comments: 6, voted: false },
];

export const changelog = [
  {
    version: "4.12.2",
    date: "2026-07-24T09:00:00Z",
    title: "Faster inbox search and two fixes",
    tags: ["Improved", "Fixed"],
    body: "Search across large conversation histories is roughly four times faster. Fixed attachment previews in Safari 18, and fixed a case where snoozed conversations could reappear early after a server restart.",
  },
  {
    version: "4.12.0",
    date: "2026-07-16T09:00:00Z",
    title: "Dark mode for the portal",
    tags: ["New"],
    body: "The customer portal now follows the visitor's system preference, or can be pinned to light or dark per portal. Thanks to everyone who voted for this on the roadmap.",
  },
  {
    version: "4.11.0",
    date: "2026-06-30T09:00:00Z",
    title: "Webhook delivery history",
    tags: ["New"],
    body: "Every delivery attempt is now retained for 30 days with its response status, latency, and error. Failed deliveries can be replayed individually or as a window.",
  },
];
