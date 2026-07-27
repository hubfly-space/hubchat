// Package auth owns credentials, sessions, and the sign-in flows.
//
// # Responsibilities
//
// Password hashing, magic links, OAuth adapters, TOTP, recovery codes, and session issue/rotate/revoke.
//
// # Boundary
//
// Never decides *what* a signed-in user may do — that is authorization. This module answers only 'who is this?'.
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
package auth
