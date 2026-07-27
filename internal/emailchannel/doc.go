// Package emailchannel owns inbound and outbound email.
//
// # Responsibilities
//
// SMTP delivery, provider webhook adapters, optional IMAP polling, threading, quote stripping, and bounce handling.
//
// # Boundary
//
// Threading uses preserved message identifiers rather than subject matching. Subject-line threading merges unrelated conversations, and support email subjects repeat constantly (§26.6).
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
package emailchannel
