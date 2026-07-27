// Package realtime owns the WebSocket gateway.
//
// # Responsibilities
//
// Connection authorization, subscription management, event fan-out, heartbeats, and resume-from-sequence.
//
// # Boundary
//
// Outbound queues are bounded and slow clients are disconnected. An unbounded queue turns one stalled browser into a server-wide memory leak (§17).
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
package realtime
