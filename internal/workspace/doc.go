// Package workspace owns tenants, members, teams, invitations, and workspace settings.
//
// # Responsibilities
//
// Creation, membership lifecycle, role assignment, branding, locale, and retention policy.
//
// # Boundary
//
// The only module permitted to create or delete a workspace row. Everything else takes a workspace id as given.
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
package workspace
