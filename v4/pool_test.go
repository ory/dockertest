package dockertest

import (
	"context"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/ory/dockertest/v4/internal/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPool(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)
	require.NotNil(t, pool)
	assert.Equal(t, DefaultMaxWait, pool.maxWait)
	assert.NotNil(t, pool.client)
}

func TestNewPoolWithContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	pool, err := NewPoolWithContext(ctx, "")
	require.NoError(t, err)
	require.NotNil(t, pool)
}

func TestPoolWithOptions(t *testing.T) {
	mockClient := &client.MockClient{}
	mockClient.On("Ping", context.Background()).Return(types.Ping{APIVersion: "1.41"}, nil)

	pool, err := NewPool("",
		WithMaxWait(5*time.Minute),
		withMockClient(mockClient),
	)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, pool.maxWait)
	assert.Equal(t, mockClient, pool.client)
}

func TestPoolPing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)

	ctx := context.Background()
	err = pool.Ping(ctx)
	assert.NoError(t, err)
}

// Helper for testing
func withMockClient(c client.Client) PoolOption {
	return func(p *Pool) error {
		p.client = c
		return nil
	}
}
