// Package database owns connection pooling, migrations, and transaction helpers.
//
// # Responsibilities
//
// Pool configuration, statement timeouts, advisory locks, migration application, and the transaction wrapper every module uses.
//
// # Boundary
//
// The migration runner takes an advisory lock so several instances starting at once apply migrations exactly once (§18).
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
package database
