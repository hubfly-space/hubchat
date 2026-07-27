// Package webhook owns outbound HTTP event delivery.
//
// # Responsibilities
//
// Endpoint management, HMAC signing, retry with backoff, delivery history, replay, and auto-disable after sustained failure.
//
// # Boundary
//
// Signatures cover a timestamp and the raw body. Endpoints are disabled after repeated failure so one dead URL cannot starve the job queue.
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
package webhook
