// Package search owns cross-entity search.
//
// # Responsibilities
//
// Workspace-scoped PostgreSQL full-text search across conversations, customers, tickets, articles, and feedback.
//
// # Boundary
//
// Results are permission-filtered in the query. A record the viewer may not read is never retrieved and then hidden (§6.17).
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
package search
