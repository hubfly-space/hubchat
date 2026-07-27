// Package audit owns the append-only change record.
//
// # Responsibilities
//
// Records authentication, configuration changes, sensitive-data access, exports, merges, and deletions.
//
// # Boundary
//
// Append-only, with no update or delete path — including for the customer-deletion workflow, where removing the record would defeat its purpose (§12).
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
package audit
