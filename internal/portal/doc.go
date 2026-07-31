// Package portal owns customer-facing hosted sites.
//
// # Responsibilities
//
// Portal configuration, custom domains and verification, navigation, and the permission model deciding what a customer may see.
//
// # Boundary
//
// Portal permissions are applied in the query, not the template. A field the customer may not see must never reach their browser.
// Portal identities and sessions are deliberately separate from agent auth;
// a customer session is never accepted by the dashboard session middleware.
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
package portal
