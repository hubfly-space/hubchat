// Package automation owns the deterministic rules engine.
//
// # Responsibilities
//
// Trigger matching, condition evaluation, action execution, dry runs, versioning, and the execution log.
//
// # Boundary
//
// Loop safety lives here: causation chains, depth limits, and per-rule rate limits (§26.7). A rule that triggers itself is cut off and logged, never run unbounded.
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
package automation
