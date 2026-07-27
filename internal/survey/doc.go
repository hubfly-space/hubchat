// Package survey owns satisfaction, effort, and recommendation surveys.
//
// # Responsibilities
//
// Survey definitions, delivery triggers, response collection, and deterministic aggregation.
//
// # Boundary
//
// Free-text answers are stored and shown verbatim. They are never classified or summarised automatically (§6.7).
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
package survey
