// Package feedback owns boards, items, votes, comments, and roadmap status.
//
// # Responsibilities
//
// Submission, moderation, voting with per-customer limits, duplicate merging, and status-change notification.
//
// # Boundary
//
// Vote integrity matters more than it looks: the counts drive roadmap decisions, so limits are enforced server-side per customer, never per browser.
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
package feedback
