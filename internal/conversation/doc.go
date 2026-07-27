// Package conversation owns threads, messages, state transitions, and assignment.
//
// # Responsibilities
//
// The operational core: create, reply, note, assign, snooze, merge, split, resolve. Owns the per-conversation sequence counter that realtime resume depends on.
//
// # Boundary
//
// State transitions are validated here, not in handlers — an invalid transition must be impossible via any entry point, including the API and automation.
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
package conversation
