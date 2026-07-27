// Package analytics owns reporting aggregation.
//
// # Responsibilities
//
// Rollups over stored events, business-hours-aware duration metrics, and CSV export.
//
// # Boundary
//
// Every metric carries its own definition string, which the interface shows on demand. A number nobody can define is a number nobody should act on (§6.18).
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
package analytics
