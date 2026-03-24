package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Error code constants for structured JSON error responses.
const (
	ErrCodeBadRequest         = "bad_request"
	ErrCodeUnauthorized       = "unauthorized"
	ErrCodeForbidden          = "forbidden"
	ErrCodeNotFound           = "not_found"
	ErrCodeDuplicate          = "duplicate"
	ErrCodeValidationError    = "validation_error"
	ErrCodeInvalidReference   = "invalid_reference"
	ErrCodeRequestTooLarge    = "request_too_large"
	ErrCodeRequestTimeout     = "request_timeout"
	ErrCodeInternalError      = "internal_error"
	ErrCodeServiceUnavailable = "service_unavailable"
	ErrCodeNotImplemented     = "not_implemented"
)

// ErrorResponse is the structured JSON error returned by all error paths.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// WriteError writes a structured JSON error response.
// Exported for use by middleware and custom handlers.
func WriteError(w http.ResponseWriter, code int, errCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(ErrorResponse{Error: errCode, Message: message}); err != nil {
		slog.Error("failed to encode error response", "error", err)
	}
}
