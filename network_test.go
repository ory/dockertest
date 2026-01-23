package dockertest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoolCreateNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)

	ctx := context.Background()
	network, err := pool.CreateNetwork(ctx, "test-network")
	require.NoError(t, err)
	defer network.Close(ctx)

	assert.NotEmpty(t, network.Network.ID)
	assert.Equal(t, "test-network", network.Network.Name)
}

func TestPoolCreateNetworkT(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)

	network := pool.CreateNetworkT(t, "test-network")
	defer network.Close(context.Background())

	assert.NotEmpty(t, network.Network.ID)
}

func TestResourceConnectToNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)

	ctx := context.Background()

	// Create network
	network, err := pool.CreateNetwork(ctx, "test-net")
	require.NoError(t, err)
	defer network.Close(ctx)

	// Create container
	resource, err := pool.Run(ctx, "postgres",
		WithTag("14-alpine"),
		WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
		WithoutReuse(),
	)
	require.NoError(t, err)
	defer resource.Close(ctx)

	// Connect to network
	err = resource.ConnectToNetwork(ctx, network)
	require.NoError(t, err)

	// Verify connection
	ip := resource.GetIPInNetwork(network)
	assert.NotEmpty(t, ip)

	// Disconnect from network
	err = resource.DisconnectFromNetwork(ctx, network)
	require.NoError(t, err)

	// Verify disconnection
	ip = resource.GetIPInNetwork(network)
	assert.Empty(t, ip)
}

func TestNetworkClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)

	ctx := context.Background()
	network, err := pool.CreateNetwork(ctx, "test-network")
	require.NoError(t, err)

	// Close network
	err = network.Close(ctx)
	assert.NoError(t, err)
}
