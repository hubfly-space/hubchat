// Package authorization answers whether an actor may perform an action.
//
// # Responsibilities
//
// Resolves a member's effective capability set from their role plus grants, and enforces inbox and team scoping.
//
// # Boundary
//
// Every service method that touches tenant data calls into here. §11.3 makes this the security boundary; the browser's `can()` is a courtesy, not a check.
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
package authorization
