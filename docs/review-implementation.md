# Review implementation

This change implements the selected review findings and product additions.

## Correctness and access

- Agent WebSockets share REST session/capability/SSO policy and reauthorize delivery,
  replay, subscriptions, and presence. Revocation closes existing connections.
- Widget browser Origin is authoritative and must agree with a supplied page URL.
  The no-Origin page-URL fallback supports non-browser SDK callers; a domain
  allowlist is not visitor identity authentication.
- Query keys include workspace scope; switching aborts old reads and discards old
  page windows. Late mutations cannot update or roll back another workspace's UI.
  Permission changes clear affected caches, and forbidden/not-found responses
  discard stale records.
- Message creation, attachment validation/linking, and events share one transaction.
- Webhook automation SQL is corrected. Stable action identities and transactional
  receipts prevent duplicate messages, tasks, scheduled work, and queued deliveries.
- Five event consumers use persisted checkpoints, startup recovery, periodic polling,
  and cross-process serialization. Queued jobs remain durable. Delivery is at least
  once; domain effects provide idempotency. Recovery depends on retained event data.
- Scheduler work uses recoverable leases and bounded retries. Workspace archive
  imports pause consumers and establish a new baseline instead of replaying old rules.
- Public webhook destinations are the default; exact private-host exceptions belong
  to deployment configuration. DNS results are checked and redirects are rejected.

## Agent and author workflows

- Inbox recent/oldest/priority sorting runs in SQL with matching opaque cursors.
  Badge queries refresh on conversation events; loading, errors, and pagination
  failures have visible retry paths.
- Knowledge-base previews, public articles, and widget articles share a safe Markdown
  renderer. Unpublished drafts autosave after 1.5 seconds; published articles require
  an explicit save/publish action. Current edits publish atomically with an expected
  version. History supports comparison and restoring content into the editor.
  Local recovery preserves unfinished edits; conflict resolution is explicit.
- Global search includes messages, customers, companies, tickets, articles, and
  feedback, with domain permissions, visible-inbox scope, type/channel/tag/date
  filters, exact ticket-number lookup, and bounded cursors.
- Manual customer creation validates contact details, detects duplicate identities,
  and keeps agent-entered email unverified.
- CSV import supports column mapping, ignored columns, duplicate previews, and an
  explicit option to skip matching emails without merging identities.
- Team visibility can be configured per inbox. Existing inboxes remain workspace-wide
  on upgrade until an administrator enables restrictions.
- Disabled-by-default workflow templates and private scheduled follow-up reminders
  extend deterministic automation.
- Reporting adds inbox/team/channel/tag breakdowns, preceding-period comparison,
  and first-response/resolution p50/p90 for observed conversation cohorts.
- Customer context cards display declared attributes using the existing masking
  policy. Integrations update those attributes through the authenticated API.
- Mobile navigation uses a drawer; inbox/detail navigation and customer context
  adapt to narrow screens. A PWA manifest and push-only service worker support
  installation and optional background notifications without caching API responses.

## Build and release

The widget uses Preact's React compatibility layer and loads Markdown on demand.
Its initial static JavaScript graph is approximately 34 KB gzip. CI enforces a
70 KiB interface budget, a 15 KiB SDK loader budget, frontend regression tests,
and the production browser journey. Browser checks run with normal origin security.

Hubchat uses AGPL-3.0-only. The complete license is in `LICENSE`. Operators providing
network access to a modified version should provide its corresponding source as
required by the license; see the license's network-interaction provision.

SMTP now requires TLS when configured for STARTTLS. Uploads check recognized file
signatures. Security documentation describes the implemented protections and
remaining workspace security/privacy settings accurately.

## Deployment settings

- `HUBCHAT_WEBHOOK_PRIVATE_HOSTS`: comma-separated exact hostnames/IPs for intentional
  private webhook targets. Empty denies private targets. No redirects are followed.
- `HUBCHAT_SMTP_ENCRYPTION`: `starttls` (default), `tls`, or explicit `none`.
- `HUBCHAT_VAPID_PUBLIC_KEY`, `HUBCHAT_VAPID_PRIVATE_KEY`,
  `HUBCHAT_VAPID_SUBSCRIBER`: optional background push configuration. Generate keys
  with `hubchat push-keys`, store the private key securely, and use an administrator
  contact address for the subscriber. Browsers need HTTPS or localhost and explicit
  permission; iOS/iPadOS users install the app first. Push respects Browser preferences.

Stop old HTTP/worker processes before this security upgrade. Back up PostgreSQL,
file storage, deployment secrets, and configuration; apply migrations 0049–0054,
then start the new binary and verify health, chat/realtime, automation, and delivery.
Migration 0049 establishes the initial consumer boundary at the existing event head;
it does not retroactively rerun pre-upgrade automation or email.

A physical PostgreSQL restore preserves checkpoints, receipts, policies, and push
subscriptions. Restore the same encryption key and file storage. Portable workspace
archives include context-card definitions, but browser subscriptions remain local
to the deployment/member and require a new opt-in on another installation.

Validation evidence is recorded by the implementation run; provider delivery still
requires the operator's SMTP/Web Push configuration.

## Continuation review and verification — 2026-10-13

The continuation first checked the existing implementation against the requested
scope. The selected fixes and feature additions were already present; they were
not reimplemented. The review found and fixed an additional push-delivery issue:
subscription markers now apply only to the current workspace member, and a visible
tab no longer discards a notification for another workspace. Regression tests cover
both cases and prevent external notification-click navigation.

Fresh verification passed:

- Frontend typechecking, linting, all builds, six shared-component tests, and two
  service-worker regressions.
- Go race tests, vet, and the complete PostgreSQL integration suite, including
  migration-upgrade, authorization, attachment rollback, search, reporting,
  reminders, and push-subscription isolation checks.
- OpenAPI coverage: 315 paths / 416 operations. SDK coverage: 18 commands / six events.
- Widget gzip budgets: 32.8 KiB across two initial JavaScript modules; loader 2.3 KiB.
- Compiled release check with normal browser security: live inbox updates, widget
  persistence after reload, portal flows, workspace switching, narrow mobile layout,
  the full HTTP/realtime journey, bounded load, and process shutdown.
- A full PostgreSQL custom-format backup was restored into a separate empty database.
  Schema/migration records matched, and a comparison of the production-journey data
  confirmed every dumped row and sequence value matched after restore.

Browser test input now uses native typing and waits for committed composer state.
Desktop navigation is tested at an explicit desktop viewport before checking mobile.
The shared preview host was unavailable, so the repository's browser acceptance
harness provided the real-browser verification. Actual external SMTP/Web Push
provider delivery still requires deployment credentials; it is not claimed here.
