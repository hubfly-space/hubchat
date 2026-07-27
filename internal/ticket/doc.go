// Package ticket owns structured cases: fields, forms linkage, numbering, and workflow.
//
// # Responsibilities
//
// Ticket lifecycle, custom field values, parent/child links, duplicate detection, and per-workspace display numbering.
//
// # Boundary
//
// Display numbers are allocated from the workspace's counter inside the creating transaction. Immutable ids and display numbers are never conflated (§6.3).
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
package ticket
