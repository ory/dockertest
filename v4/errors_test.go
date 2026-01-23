package dockertest

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name     string
		sentinel error
	}{
		{"ErrConnectionRefused", ErrConnectionRefused},
		{"ErrImagePullFailed", ErrImagePullFailed},
		{"ErrContainerCreateFailed", ErrContainerCreateFailed},
		{"ErrContainerStartFailed", ErrContainerStartFailed},
		{"ErrExecFailed", ErrExecFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotNil(t, tt.sentinel)
			assert.Error(t, tt.sentinel)
		})
	}
}

func TestErrorWrapping(t *testing.T) {
	cause := errors.New("underlying error")
	wrapped := fmt.Errorf("failed to pull image: %w", ErrImagePullFailed)

	assert.True(t, errors.Is(wrapped, ErrImagePullFailed))

	doubleWrapped := fmt.Errorf("operation failed: %w", wrapped)
	assert.True(t, errors.Is(doubleWrapped, ErrImagePullFailed))

	withCause := fmt.Errorf("pull postgres:14: %w: %w", ErrImagePullFailed, cause)
	assert.True(t, errors.Is(withCause, ErrImagePullFailed))
}
