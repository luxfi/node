// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

// AppError is an application-level error with a code and message
type AppError struct {
	Code    int
	Message string
}

// Error implements the error interface
func (e *AppError) Error() string {
	return e.Message
}

// ErrUndefined is a generic undefined error
var ErrUndefined = &AppError{
	Code:    -1,
	Message: "undefined error",
}