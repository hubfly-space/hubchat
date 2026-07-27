# Security

Hubchat stores other people's customer conversations. That framing decides most
of what follows.

---

## 1. Threat model

What we defend against, roughly in order of how much damage it does:

| Threat | Consequence | Primary control |
|---|---|---|
| Cross-tenant data access | One workspace reads another's conversations | Workspace scoping on every query |
| Forged customer identity | An attacker impersonates a customer to an agent | Signed identity tokens |
| Privilege escalation | An agent performs owner-only actions | Service-layer capability checks |
| Widget origin abuse | A third-party site embeds a workspace's widget | Per-widget domain allowlist |
| Secret exposure | Keys leak via logs, exports, or diagnostics | Hashing, encryption, redaction |
| XSS in content | Stored script executes in an agent's session | Sanitisation and CSP |

## 2. Tenant isolation

**Every query is scoped by workspace. There are no exceptions.**

```go
// yes
WHERE workspace_id = $1 AND id = $2

// no
WHERE id = $1
```

A missing workspace predicate is treated as a **critical defect**, not a bug. It
does not wait for the next sprint.

Three layers back this up:

1. **Schema** — `workspace_id` leads every primary lookup index, so a query that
   forgets it is slow enough to be noticed.
2. **Service layer** — the workspace comes from the authenticated context, never
   from a request parameter. A client cannot ask for a different tenant.
3. **Tests** — every resource type has a test that reads it from the wrong
   workspace and asserts a 404.

Isolation applies at every layer, not just SQL: WebSocket subscriptions, file
storage paths, cache keys, background jobs, and search indexes are all
workspace-scoped.

**Cross-tenant access returns 404, not 403.** A 403 confirms the record exists.

## 3. Authorization

Roles are bundles of capabilities; capabilities are the unit of authorization.

```go
if !authz.Can(ctx, actor, authz.ConversationDelete) {
    return ErrForbidden
}
```

- Checked in the **service layer**, on every mutation, every time.
- The browser's `can()` decides whether to render a control. It is a courtesy,
  not a boundary. Assume every hidden button is being called directly.
- Owner is short-circuited rather than enumerated, so a new capability can never
  accidentally leave the owner without it.
- Inbox and team access narrows results **in the query**. A conversation an
  agent may not see is never retrieved and then filtered.

## 4. Customer identity

This is the sharpest edge in the product.

**A customer id from the browser is a claim, not proof.** Anyone can open a
console and claim to be anyone.

Verified identity requires a token signed server-side with a secret the browser
never sees:

```json
{
  "workspace_id": "wrk_01hq7x",
  "external_id": "u_44192",
  "email": "customer@example.com",
  "iat": 1774526400,
  "exp": 1774530000,
  "jti": "single-use-nonce"
}
```

Verified on receipt: signature, workspace match, issued-in-the-past, not
expired, nonce unseen within the replay window, and that the external id is not
already claimed by a different verified identity.

The agent interface shows **Verified**, **Unverified**, or **Anonymous**, and
agents are trained to treat them differently. The badge is not decoration — it
is the signal that decides whether account details may be discussed.

### Merging

Hubchat **never** merges customers on weak signals — similar names, shared IP
addresses, or a matching unverified email. Merges require a strong signal
(identity token, verified email, external id, or an explicit human action),
retain provenance, are written to the audit log, and are reversible for a
bounded window.

The failure mode this prevents is showing one customer another customer's
conversation history, which is unrecoverable once it has happened.

## 5. Secrets

| Secret | At rest |
|---|---|
| Passwords | Argon2id |
| Session tokens | SHA-256 hash only |
| API keys | Hash only; full key shown once, at creation |
| Webhook signing secrets | Encrypted with the deployment key |
| Integration credentials | Encrypted with the deployment key |
| `HUBCHAT_SECRET_KEY` | Environment only; never in the database |

- **Never logged.** Not at debug level, not in a panic, not in an error message.
- `config check` and `doctor` print the **redacted** configuration, because that
  output gets pasted into issue reports.
- Rotation is supported for API keys and webhook secrets, with an overlap window
  so a rotation does not drop events mid-deploy.

## 6. Web security

- HTTPS, HSTS in production.
- Secure, `HttpOnly`, `SameSite` cookies.
- CSRF tokens on cookie-authenticated state changes. Bearer-token API calls do
  not need them and do not get them.
- CSP per surface — the dashboard, portal, widget, and API have genuinely
  different needs. See `internal/httpserver/middleware.go`.
- `X-Frame-Options: DENY` on the dashboard. Clickjacking it means clickjacking
  every conversation in the workspace.
- Request bodies capped before a handler reads them.
- Rate limiting per key, per workspace, per IP.
- Brute-force lockout on repeated authentication failure, recorded in the audit
  log.

## 7. Widget origin control

The widget's public key is safe to expose — it identifies which widget to load
and grants no read access.

Origin control comes from the per-widget domain allowlist, checked when the
configuration is requested. A disallowed origin gets nothing: no inbox names, no
welcome copy, and no interface download. An attacker embedding a stolen public
key on their own site learns nothing about the workspace.

## 8. Content safety

- Message bodies and article content are sanitised on **render**, with an
  allowlist. Storing raw input keeps the record faithful; the sanitiser is what
  changes when a new vector is found.
- Attachments are validated by extension and MIME type, size-capped, stored
  under random tenant-prefixed names, and served with a download disposition.
- Path traversal is impossible by construction: object names are generated, not
  derived from user input.
- An external malware scanner can be attached through a generic hook.

## 9. Sensitive fields

A custom field marked sensitive is:

- masked in the interface,
- excluded from search indexes,
- excluded from exports,
- readable only with `customer.read_sensitive`,
- **audited on every reveal**,
- optionally encrypted at the application layer,
- subject to shorter retention.

Marking a field sensitive applies retroactively: existing values are masked
immediately and dropped from the index on the next rebuild.

## 10. Reporting a vulnerability

Do not open a public issue. Email `security@hubchat.dev` with reproduction
steps. We acknowledge within 72 hours and aim to ship a fix or a mitigation
within 14 days for anything that crosses a tenant boundary.
