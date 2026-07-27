// Package sla owns service-level policies and their timers.
//
// # Responsibilities
//
// Policy definitions, business-hours calendars, timer start/pause/resume, breach detection, and escalation.
//
// # Boundary
//
// Timers advance in business hours, not wall-clock. The calendar is the arithmetic, which is why it lives beside the policy rather than in a shared time helper.
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
package sla
