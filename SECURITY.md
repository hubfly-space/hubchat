# Security policy

## Reporting a vulnerability

Please do not open a public issue for a suspected security vulnerability.
Email `security@hubchat.space` with:

- a short description of the impact;
- the affected version or commit;
- reproduction steps or a minimal proof of concept;
- any relevant logs, request IDs, or workspace-scope details.

We will acknowledge reports within five business days and coordinate a fix,
disclosure date, and credit with the reporter where appropriate. Do not include
real customer data or production secrets in a report.

## Security boundaries

Workspace isolation is a critical boundary. Every API, background job,
WebSocket subscription, file operation, and customer-facing token must remain
workspace-scoped. See [the security guide](docs/security.md) for the threat
model and identity rules.

## Supported versions

The latest release receives security fixes. Older releases may receive a fix
only when the issue is severe and the patch can be applied safely without a
version-specific migration.
