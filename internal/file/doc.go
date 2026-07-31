// Package file owns attachment storage and access control.
//
// # Responsibilities
//
// Upload validation, local and S3-compatible backends, signed URLs, checksums, and abandoned-upload cleanup.
//
// The local backend is implemented first. Metadata is kept in PostgreSQL and
// is the authorization boundary; callers never open a storage key directly.
//
// # Boundary
//
// Every download is authorized. Object names are random and tenant-prefixed so a leaked URL is not a directory listing (§10.3).
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
package file
