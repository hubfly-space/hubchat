// Package app wires the modules together and owns process lifecycle.
//
// # Responsibilities
//
// Constructs every service in dependency order, starts the roles this process runs, and coordinates graceful shutdown.
//
// # Boundary
//
// The only package that knows the full dependency graph. Modules depend on interfaces; this is where the concrete types meet.
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
package app
