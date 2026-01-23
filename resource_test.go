package dockertest

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceGetPort(t *testing.T) {
	r := &Resource{
		Container: types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID: "test123",
			},
			NetworkSettings: &types.NetworkSettings{
				NetworkSettingsBase: types.NetworkSettingsBase{
					Ports: nat.PortMap{
						"5432/tcp": []nat.PortBinding{
							{HostPort: "12345"},
						},
					},
				},
			},
		},
	}

	port := r.GetPort("5432/tcp")
	assert.Equal(t, "12345", port)

	// Non-existent port
	port = r.GetPort("3306/tcp")
	assert.Equal(t, "", port)
}

func TestResourceGetBoundIP(t *testing.T) {
	tests := []struct {
		name     string
		hostIP   string
		expected string
	}{
		{
			name:     "0.0.0.0 becomes localhost",
			hostIP:   "0.0.0.0",
			expected: "localhost",
		},
		{
			name:     "empty becomes localhost",
			hostIP:   "",
			expected: "localhost",
		},
		{
			name:     "specific IP unchanged",
			hostIP:   "192.168.1.1",
			expected: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Resource{
				Container: types.ContainerJSON{
					NetworkSettings: &types.NetworkSettings{
						NetworkSettingsBase: types.NetworkSettingsBase{
							Ports: nat.PortMap{
								"5432/tcp": []nat.PortBinding{
									{HostIP: tt.hostIP, HostPort: "12345"},
								},
							},
						},
					},
				},
			}

			ip := r.GetBoundIP("5432/tcp")
			assert.Equal(t, tt.expected, ip)
		})
	}
}

func TestResourceGetHostPort(t *testing.T) {
	r := &Resource{
		Container: types.ContainerJSON{
			NetworkSettings: &types.NetworkSettings{
				NetworkSettingsBase: types.NetworkSettingsBase{
					Ports: nat.PortMap{
						"5432/tcp": []nat.PortBinding{
							{HostIP: "0.0.0.0", HostPort: "12345"},
						},
					},
				},
			},
		},
	}

	hostPort := r.GetHostPort("5432/tcp")
	assert.Equal(t, "localhost:12345", hostPort)
}

func TestResourceGetIPInNetwork(t *testing.T) {
	r := &Resource{
		Container: types.ContainerJSON{
			NetworkSettings: &types.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					"testnet": &network.EndpointSettings{
						IPAddress: "172.18.0.2",
					},
				},
			},
		},
	}

	network := &Network{
		Network: types.NetworkResource{
			Name: "testnet",
		},
	}

	ip := r.GetIPInNetwork(network)
	assert.Equal(t, "172.18.0.2", ip)

	// Non-existent network
	otherNet := &Network{
		Network: types.NetworkResource{
			Name: "othernet",
		},
	}
	ip = r.GetIPInNetwork(otherNet)
	assert.Equal(t, "", ip)
}

func TestResourceExec(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)

	ctx := context.Background()
	resource, err := pool.Run(ctx, "postgres",
		WithTag("14-alpine"),
		WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
		WithoutReuse(),
	)
	require.NoError(t, err)
	defer resource.Close(ctx)

	// Execute command
	exitCode, err := resource.Exec(ctx, []string{"echo", "hello"})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestResourceExecT(t *testing.T) {
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

	// Execute command - fails test on error
	exitCode := resource.ExecT(t, []string{"echo", "hello"})
	assert.Equal(t, 0, exitCode)
}

func TestResourceExecNonZeroExit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	pool, err := NewPool("")
	require.NoError(t, err)

	ctx := context.Background()
	resource, err := pool.Run(ctx, "postgres",
		WithTag("14-alpine"),
		WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
		WithoutReuse(),
	)
	require.NoError(t, err)
	defer resource.Close(ctx)

	// Execute command that fails
	exitCode, err := resource.Exec(ctx, []string{"false"})
	require.NoError(t, err)      // No exec error
	assert.Equal(t, 1, exitCode) // But exit code is non-zero
}
