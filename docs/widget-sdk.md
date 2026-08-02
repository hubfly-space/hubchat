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
Hubchat("open"); // alias for show
Hubchat("hide");
Hubchat("close"); // alias for hide
Hubchat("toggle");
Hubchat("startConversation", { message: "I need help with billing" });
Hubchat("openArticle", { slug: "billing/refunds" });
Hubchat("openForm", { slug: "contact-support" });
Hubchat("openTicketForm", { slug: "contact-support" });
Hubchat("openFeedback", { slug: "roadmap" });
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
Hubchat("on", { event: "ready", handler: () => console.log("Hubchat is ready") });

// Register an explicit host-owned command. The dashboard can invoke this
// binding from an active conversation; Hubchat never evaluates JavaScript.
Hubchat("bind", {
  name: "reload_page",
  handler: ({ reason }) => {
    console.log("Reload requested for", reason);
    window.location.reload();
  }
});
Hubchat("unbind", { name: "reload_page" });
```

`open`/`close` are aliases for `show`/`hide`. `openForm` and `openFeedback`
remain supported alongside the more explicit `openTicketForm` and
`openFeedbackForm` names.

The widget keeps the visitor token and active conversation in browser-local
storage, so reopening or refreshing the page resumes the same conversation.
The widget also records a bounded, privacy-reduced page journey and device
context for the customer 360 view.

Commands are host-owned actions. A binding registered with `bind` is invoked
only by an authenticated dashboard agent through an active visitor
conversation; the server never evaluates JavaScript or accepts a script body.
Commands sent while the visitor is temporarily offline are retained for two
minutes and claimed once when the widget reconnects. Reconnect delivery is
cursor-paged internally, so a burst of commands is not truncated at the first
page. Handlers should be idempotent because a host may retry a failed network
acknowledgement.

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
`conversation:started`, `unread:changed`, and `command`. Event payloads are intentionally
small and contain no internal notes or sensitive dashboard-only fields.

## Contract and delivery behavior

- The API is available only after `boot` has supplied a public widget key.
- Calls made before lazy loading completes replay in their original order.
- If the public configuration request fails, `boot` remains retryable; calls
  already queued are retained and replay after the next successful boot.
- `context` and `track` create visitor events; `update` writes declared customer
  attributes through the server-side metadata validator.
- The loader does not expose secrets and does not use customer identity as a
  browser-held authentication credential.
- The REST widget endpoints preserve the standard error envelope and request
  ID contract; the browser adapter intentionally keeps failures non-fatal to
  the host page.
