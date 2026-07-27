// Package customer owns people, companies, identities, attributes, and events.
//
// # Responsibilities
//
// Customer and company records, identity verification and linking, the metadata allowlist, and application event ingestion.
//
// # Boundary
//
// Identity merging is the sharpest edge in the product (§26.3). Merges require an explicit strong signal, keep provenance, and are reversible for a bounded window.
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
package customer
