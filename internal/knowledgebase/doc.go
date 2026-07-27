// Package knowledgebase owns collections, articles, revisions, and article search.
//
// # Responsibilities
//
// Authoring workflow, publishing and scheduling, revision history, language variants, and PostgreSQL full-text ranking.
//
// # Boundary
//
// Ranking is deterministic and explainable (§3.6). A search result must be traceable to a term match, never to a model.
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
package knowledgebase
