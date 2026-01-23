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

func TestPoolRun(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)

	ctx := context.Background()
	resource, err := pool.Run(ctx, "postgres",
		WithTag("14-alpine"),
		WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
	)
	require.NoError(t, err)
	require.NotNil(t, resource)
	defer resource.Close(ctx)

	// Verify container is running
	assert.NotEmpty(t, resource.Container.ID)
	assert.Equal(t, "running", resource.Container.State.Status)
}

func TestPoolRunWithReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Clear registry
	registry = newContainerRegistry()
	defer func() { _ = Cleanup() }()

	pool, err := NewPool("")
	require.NoError(t, err)

	ctx := context.Background()

	// First run
	r1, err := pool.Run(ctx, "postgres",
		WithTag("14-alpine"),
		WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
	)
	require.NoError(t, err)
	id1 := r1.Container.ID

	// Second run with same options should reuse
	r2, err := pool.Run(ctx, "postgres",
		WithTag("14-alpine"),
		WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
	)
	require.NoError(t, err)
	id2 := r2.Container.ID

	// Should be same container
	assert.Equal(t, id1, id2)
}

func TestPoolRunWithoutReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	// Clear registry
	registry = newContainerRegistry()
	defer func() { _ = Cleanup() }()

	pool, err := NewPool("")
	require.NoError(t, err)

	ctx := context.Background()

	// First run
	r1, err := pool.Run(ctx, "postgres",
		WithTag("14-alpine"),
		WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
		WithoutReuse(),
	)
	require.NoError(t, err)
	defer r1.Close(ctx)
	id1 := r1.Container.ID

	// Second run with same options but no reuse
	r2, err := pool.Run(ctx, "postgres",
		WithTag("14-alpine"),
		WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
		WithoutReuse(),
	)
	require.NoError(t, err)
	defer r2.Close(ctx)
	id2 := r2.Container.ID

	// Should be different containers
	assert.NotEqual(t, id1, id2)
}

func TestPoolRunT(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)

	resource := pool.RunT(t, "postgres",
		WithTag("14-alpine"),
		WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
		WithoutReuse(),
	)
	defer resource.Close(context.Background())

	assert.NotEmpty(t, resource.Container.ID)
}

// Helper for testing
func withMockClient(c client.Client) PoolOption {
	return func(p *Pool) error {
		p.client = c
		return nil
	}
}
