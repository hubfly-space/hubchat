package httpserver

import (
	"encoding/json"
	"net/http"
)

// APIError is the one error shape the API ever returns (§16).
//
// Consistency here is not tidiness — it is what lets an SDK, a webhook
// consumer, and the dashboard all handle failure with one code path. A handler
// that invents its own error body breaks every one of them.
type APIError struct {
	Error APIErrorBody `json:"error"`
}

type APIErrorBody struct {
	// Code is stable and machine-readable. It is part of the API contract:
	// renaming one is a breaking change.
	Code string `json:"code"`
	// Message is for a human. It is safe to show a customer, which means it
	// never contains a stack trace, a SQL fragment, or an internal identifier.
	Message string `json:"message"`
	// RequestID lets support correlate a report with the server logs.
	RequestID string `json:"request_id"`
	// Details carries field-level validation problems. Empty for most errors.
	Details map[string]any `json:"details,omitempty"`
}

// WriteError renders the standard error body.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	WriteErrorDetails(w, r, status, code, message, nil)
}

// WriteErrorDetails renders the standard error body with field-level detail.
func WriteErrorDetails(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code, message string,
	details map[string]any,
) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(APIError{
		Error: APIErrorBody{
			Code:      code,
			Message:   message,
			RequestID: RequestIDFrom(r.Context()),
			Details:   details,
		},
	})
}

// WriteJSON renders a successful response.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Common error codes. Handlers should reach for one of these before inventing
// a new one; the set staying small is what makes it useful to consumers.
const (
	CodeBadRequest      = "bad_request"
	CodeValidationError = "validation_error"
	CodeUnauthorized    = "unauthorized"
	CodeForbidden       = "forbidden"
	CodeNotFound        = "not_found"
	CodeConflict        = "conflict"
	CodeRateLimited     = "rate_limited"
	CodePayloadTooLarge = "payload_too_large"
	CodeInternalError   = "internal_error"
	CodeUnavailable     = "service_unavailable"
)
