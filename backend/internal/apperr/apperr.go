// Package apperr defines error kinds shared by feature services, so handlers
// can map them to HTTP status codes in one place (httpx.WriteError).
package apperr

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidInput: the request is malformed or violates a field rule (400).
	ErrInvalidInput = errors.New("invalid input")
	// ErrNotFound: missing, or not visible to the caller (404). Ownership
	// failures also use this so other teachers' data is not revealed.
	ErrNotFound = errors.New("not found")
	// ErrConflict: valid request that the current state does not allow (409).
	ErrConflict = errors.New("conflict")
	// ErrForbidden: the caller may see the resource but not do this now (403).
	ErrForbidden = errors.New("forbidden")
	// ErrTooLarge: the uploaded content exceeds a size limit (413).
	ErrTooLarge = errors.New("too large")
)

// Invalid returns an ErrInvalidInput with a message for the client.
func Invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

// NotFound returns an ErrNotFound naming the resource.
func NotFound(what string) error {
	return fmt.Errorf("%w: %s", ErrNotFound, what)
}

// Conflict returns an ErrConflict with a message for the client.
func Conflict(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrConflict, fmt.Sprintf(format, args...))
}

// TooLarge returns an ErrTooLarge with a message for the client.
func TooLarge(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrTooLarge, fmt.Sprintf(format, args...))
}

// Forbidden returns an ErrForbidden with a message for the client.
func Forbidden(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrForbidden, fmt.Sprintf(format, args...))
}
