// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import (
	"errors"
	"fmt"
)

// ErrorType represents the category of error.
type ErrorType int

const (
	// ErrTypeUnknown represents an unknown error type.
	ErrTypeUnknown ErrorType = iota
	// ErrTypeContainerNotFound indicates a container was not found.
	ErrTypeContainerNotFound
	// ErrTypeImageNotFound indicates an image was not found.
	ErrTypeImageNotFound
	// ErrTypeNetworkNotFound indicates a network was not found.
	ErrTypeNetworkNotFound
	// ErrTypeTimeout indicates an operation timed out.
	ErrTypeTimeout
	// ErrTypeConnectionRefused indicates a connection was refused.
	ErrTypeConnectionRefused
	// ErrTypeImagePullFailed indicates an image pull operation failed.
	ErrTypeImagePullFailed
	// ErrTypeContainerCreateFailed indicates container creation failed.
	ErrTypeContainerCreateFailed
	// ErrTypeContainerStartFailed indicates container start failed.
	ErrTypeContainerStartFailed
	// ErrTypeExecFailed indicates an exec operation failed.
	ErrTypeExecFailed
)

// Error represents a typed error from dockertest operations.
type Error struct {
	// Type is the category of error.
	Type ErrorType
	// Message is a human-readable description.
	Message string
	// Cause is the underlying error that caused this error.
	Cause error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap returns the underlying cause error.
func (e *Error) Unwrap() error {
	return e.Cause
}

// Is implements error matching for errors.Is.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Type == t.Type
}

// wrapError creates a new Error with the given type, message, and cause.
func wrapError(errType ErrorType, message string, cause error) *Error {
	return &Error{
		Type:    errType,
		Message: message,
		Cause:   cause,
	}
}

// IsNotFound returns true if the error is a "not found" error.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Type == ErrTypeContainerNotFound ||
			e.Type == ErrTypeImageNotFound ||
			e.Type == ErrTypeNetworkNotFound
	}
	return false
}

// IsTimeout returns true if the error is a timeout error.
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Type == ErrTypeTimeout
	}
	return false
}

// IsConnectionError returns true if the error is a connection-related error.
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Type == ErrTypeConnectionRefused || e.Type == ErrTypeTimeout
	}
	return false
}
