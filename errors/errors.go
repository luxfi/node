// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

// Package errors provides common error types and utilities for the Lux node.
package errors

import (
	"errors"
	"fmt"
)

// Common sentinel errors used throughout the codebase
var (
	// Database errors
	ErrNotFound     = errors.New("not found")
	ErrClosed       = errors.New("closed")
	ErrCorrupted    = errors.New("corrupted")
	ErrReadOnly     = errors.New("read only")
	ErrDiskFull     = errors.New("disk full")
	ErrInvalidKey   = errors.New("invalid key")
	ErrInvalidValue = errors.New("invalid value")

	// Network errors
	ErrTimeout          = errors.New("timeout")
	ErrConnectionClosed = errors.New("connection closed")
	ErrConnectionReset  = errors.New("connection reset")
	ErrNoRoute          = errors.New("no route to host")
	ErrRefused          = errors.New("connection refused")
	ErrNetworkDown      = errors.New("network down")

	// Validation errors
	ErrInvalidInput     = errors.New("invalid input")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrInvalidChecksum  = errors.New("invalid checksum")
	ErrInvalidFormat    = errors.New("invalid format")
	ErrMissingField     = errors.New("missing required field")
	ErrFieldTooLarge    = errors.New("field exceeds maximum size")

	// State errors
	ErrNotInitialized = errors.New("not initialized")
	ErrAlreadyExists  = errors.New("already exists")
	ErrNotSupported   = errors.New("not supported")
	ErrDeprecated     = errors.New("deprecated")
	ErrConflict       = errors.New("conflict")
	ErrCanceled       = errors.New("canceled")

	// Resource errors
	ErrResourceExhausted = errors.New("resource exhausted")
	ErrQuotaExceeded     = errors.New("quota exceeded")
	ErrRateLimited       = errors.New("rate limited")
	ErrOutOfMemory       = errors.New("out of memory")

	// Permission errors
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrAccessDenied = errors.New("access denied")
)

// Error categories for grouping related errors
type Category string

const (
	CategoryDatabase   Category = "database"
	CategoryNetwork    Category = "network"
	CategoryValidation Category = "validation"
	CategoryState      Category = "state"
	CategoryResource   Category = "resource"
	CategoryPermission Category = "permission"
	CategoryInternal   Category = "internal"
	CategoryUnknown    Category = "unknown"
)

// WrappedError provides context around an error
type WrappedError struct {
	Err      error
	Category Category
	Message  string
	Context  map[string]interface{}
}

// Error implements the error interface
func (e *WrappedError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("[%s] %s: %v", e.Category, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %v", e.Category, e.Err)
}

// Unwrap returns the underlying error
func (e *WrappedError) Unwrap() error {
	return e.Err
}

// Is checks if the error matches a target error
func (e *WrappedError) Is(target error) bool {
	return errors.Is(e.Err, target)
}

// Wrap creates a new WrappedError with the given category and message
func Wrap(err error, category Category, message string) error {
	if err == nil {
		return nil
	}
	return &WrappedError{
		Err:      err,
		Category: category,
		Message:  message,
		Context:  make(map[string]interface{}),
	}
}

// WrapWithContext creates a new WrappedError with context information
func WrapWithContext(err error, category Category, message string, context map[string]interface{}) error {
	if err == nil {
		return nil
	}
	return &WrappedError{
		Err:      err,
		Category: category,
		Message:  message,
		Context:  context,
	}
}

// IsNotFound checks if an error is a "not found" error
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsClosed checks if an error indicates a closed resource
func IsClosed(err error) bool {
	return errors.Is(err, ErrClosed)
}

// IsTimeout checks if an error is a timeout
func IsTimeout(err error) bool {
	return errors.Is(err, ErrTimeout)
}

// IsTemporary checks if an error is temporary and can be retried
func IsTemporary(err error) bool {
	// Check for common temporary errors
	if errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrResourceExhausted) {
		return true
	}

	// Check if error implements Temporary() method
	type temporary interface {
		Temporary() bool
	}
	if temp, ok := err.(temporary); ok {
		return temp.Temporary()
	}

	return false
}

// IsPermanent checks if an error is permanent and should not be retried
func IsPermanent(err error) bool {
	// Check for common permanent errors
	return errors.Is(err, ErrNotSupported) ||
		errors.Is(err, ErrDeprecated) ||
		errors.Is(err, ErrInvalidInput) ||
		errors.Is(err, ErrInvalidSignature) ||
		errors.Is(err, ErrInvalidFormat) ||
		errors.Is(err, ErrForbidden) ||
		errors.Is(err, ErrUnauthorized)
}

// GetCategory returns the category of an error
func GetCategory(err error) Category {
	var wrapped *WrappedError
	if errors.As(err, &wrapped) {
		return wrapped.Category
	}

	// Try to infer category from error type
	switch {
	case errors.Is(err, ErrNotFound) || errors.Is(err, ErrClosed):
		return CategoryDatabase
	case errors.Is(err, ErrTimeout) || errors.Is(err, ErrConnectionClosed):
		return CategoryNetwork
	case errors.Is(err, ErrInvalidInput) || errors.Is(err, ErrInvalidSignature):
		return CategoryValidation
	case errors.Is(err, ErrNotInitialized) || errors.Is(err, ErrAlreadyExists):
		return CategoryState
	case errors.Is(err, ErrResourceExhausted) || errors.Is(err, ErrOutOfMemory):
		return CategoryResource
	case errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrForbidden):
		return CategoryPermission
	default:
		return CategoryUnknown
	}
}

// Multi combines multiple errors into a single error
type Multi struct {
	Errors []error
}

// Error implements the error interface
func (m *Multi) Error() string {
	if len(m.Errors) == 0 {
		return "no errors"
	}
	if len(m.Errors) == 1 {
		return m.Errors[0].Error()
	}
	return fmt.Sprintf("multiple errors: %v", m.Errors)
}

// Add adds an error to the multi-error
func (m *Multi) Add(err error) {
	if err != nil {
		m.Errors = append(m.Errors, err)
	}
}

// Err returns nil if there are no errors, otherwise returns the Multi error
func (m *Multi) Err() error {
	if len(m.Errors) == 0 {
		return nil
	}
	return m
}

// Join combines multiple errors into a single error
func Join(errs ...error) error {
	multi := &Multi{}
	for _, err := range errs {
		multi.Add(err)
	}
	return multi.Err()
}
