// Package form owns intake forms and their submissions.
//
// # Responsibilities
//
// Form definitions, conditional field logic, validation, spam protection, and routing a submission to its destination.
//
// # Boundary
//
// A submission may become a ticket, conversation, feedback item, or survey response. The form's purpose decides which; the caller does not.
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
package form
