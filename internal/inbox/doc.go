// Package inbox owns inboxes, channels, and routing configuration.
//
// # Responsibilities
//
// Inbox CRUD, team access, channel enablement, and the deterministic assignment strategies from §6.12.
//
// # Boundary
//
// Routing decisions live here so round-robin and least-active are unit-testable without a conversation.
//
// # Rules that apply to every module
//
//   - Database access for the entities this module owns happens here and
//     nowhere else. Another module that needs them calls a service method.
//   - Every query is scoped by workspace. A missing workspace predicate is a
//     critical security defect, not a bug (§11.6).
//   - HTTP handlers hold no business logic; they call into this package.
//   - Cross-module work is an explicit service call or a domain event, never a
//     reach into another module's tables.
package inbox
