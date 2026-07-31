// Package notification owns agent and customer notification delivery.
//
// # Responsibilities
//
// Preference resolution and event-backed fan-out to the durable notification
// inbox. Email-enabled alerts are also handed to the durable email job queue;
// browser adapters and digest/quiet-hour policies remain separate work.
//
// # Boundary
//
// Preference resolution is layered: workspace default, then member override. The resolved decision is computed here so no delivery path re-implements it.
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
package notification
