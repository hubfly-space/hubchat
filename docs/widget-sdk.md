# Hubchat JavaScript SDK

The Hubchat widget exposes a small, versioned browser API through
`window.Hubchat`. The loader is safe to install before the page is ready: calls
are queued until the workspace configuration and widget application load.

```html
<script>
  window.Hubchat = window.Hubchat || function () {
    (window.Hubchat.q = window.Hubchat.q || []).push(arguments);
  };
  window.Hubchat("boot", { key: "wgt_public_key" });
</script>
<script async src="https://support.example.com/widget/v1.js"></script>
```

The checked-in declaration file is
[`web/widget/public/v1.d.ts`](../web/widget/public/v1.d.ts). It can be copied
into a TypeScript integration or used as the source for an application-local
global declaration.

## Commands

```js
Hubchat("show");
Hubchat("startConversation", { message: "I need help with billing" });
Hubchat("openArticle", { slug: "billing/refunds" });
Hubchat("openTicketForm", { slug: "contact-support" });
Hubchat("openFeedbackForm", { slug: "roadmap" });

Hubchat("identify", {
  name: "Ada Lovelace",
  email: "ada@example.com",
  external_id: "customer-42",
  signed_token: "workspace-issued-token",
  attributes: { plan: "pro" }
});

Hubchat("context", { page: "pricing", plan: "pro" });
Hubchat("update", { attributes: { plan: "pro" } });
Hubchat("track", { type: "checkout.started", payload: { currency: "RWF" } });
Hubchat("reset");
```

`open`/`close` are aliases for `show`/`hide`. `openForm` and `openFeedback`
remain supported alongside the more explicit `openTicketForm` and
`openFeedbackForm` names.

Customer attributes sent through `identify` or `update` are not arbitrary
metadata writes. The workspace must declare each key and allow the `js_sdk`
source; the normal metadata type, value, and attribute-count limits still
apply. Invalid attributes are rejected without linking the visitor.

`signed_token` is optional. Unsigned identity creates an unverified customer;
the workspace's backend can issue a signed token when the integration needs
verified identity and external-ID matching.

## Lifecycle events

```js
Hubchat("on", {
  event: "unread:changed",
  handler: ({ count }) => updateBadge(count)
});
```

Supported events are `ready`, `open`, `close`, `message:received`,
`conversation:started`, and `unread:changed`. Event payloads are intentionally
small and contain no internal notes or sensitive dashboard-only fields.

## Contract and delivery behavior

- The API is available only after `boot` has supplied a public widget key.
- Calls made before lazy loading completes replay in their original order.
- `context` and `track` create visitor events; `update` writes declared customer
  attributes through the server-side metadata validator.
- The loader does not expose secrets and does not use customer identity as a
  browser-held authentication credential.
- The REST widget endpoints preserve the standard error envelope and request
  ID contract; the browser adapter intentionally keeps failures non-fatal to
  the host page.
