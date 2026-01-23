package dockertest

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorType(t *testing.T) {
	err := &Error{
		Type:    ErrTypeContainerNotFound,
		Message: "container abc123 not found",
		Cause:   errors.New("original error"),
	}

	assert.Equal(t, "container abc123 not found: original error", err.Error())
	assert.Equal(t, ErrTypeContainerNotFound, err.Type)
	assert.True(t, errors.Is(err, err.Cause))
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("original error")
	err := &Error{
		Type:    ErrTypeTimeout,
		Message: "timeout waiting for container",
		Cause:   cause,
	}

	assert.Equal(t, cause, errors.Unwrap(err))
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "container not found",
			err:      &Error{Type: ErrTypeContainerNotFound},
			expected: true,
		},
		{
			name:     "image not found",
			err:      &Error{Type: ErrTypeImageNotFound},
			expected: true,
		},
		{
			name:     "network not found",
			err:      &Error{Type: ErrTypeNetworkNotFound},
			expected: true,
		},
		{
			name:     "timeout error",
			err:      &Error{Type: ErrTypeTimeout},
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsNotFound(tt.err))
		})
	}
}

func TestIsTimeout(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "timeout error",
			err:      &Error{Type: ErrTypeTimeout},
			expected: true,
		},
		{
			name:     "not found error",
			err:      &Error{Type: ErrTypeContainerNotFound},
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsTimeout(tt.err))
		})
	}
}

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "connection refused",
			err:      &Error{Type: ErrTypeConnectionRefused},
			expected: true,
		},
		{
			name:     "timeout is also connection error",
			err:      &Error{Type: ErrTypeTimeout},
			expected: true,
		},
		{
			name:     "not found error",
			err:      &Error{Type: ErrTypeContainerNotFound},
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsConnectionError(tt.err))
		})
	}
}

func TestWrapError(t *testing.T) {
	cause := errors.New("underlying error")
	err := wrapError(ErrTypeImagePullFailed, "failed to pull postgres:14", cause)

	assert.Equal(t, ErrTypeImagePullFailed, err.Type)
	assert.Equal(t, "failed to pull postgres:14", err.Message)
	assert.Equal(t, cause, err.Cause)
	assert.Contains(t, err.Error(), "failed to pull postgres:14")
	assert.Contains(t, err.Error(), "underlying error")
}
