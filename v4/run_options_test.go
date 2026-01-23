package dockertest

import (
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunOptions(t *testing.T) {
	cfg := &runConfig{}

	// Apply options
	opts := []RunOption{
		WithName("test-container"),
		WithTag("14"),
		WithEnv([]string{"FOO=bar", "BAZ=qux"}),
		WithCmd([]string{"echo", "hello"}),
		WithExposedPorts("5432", "8080"),
		WithPrivileged(true),
		WithPlatform("linux/amd64"),
		WithTTY(true),
		WithReuseID("custom-id"),
		WithoutReuse(),
	}

	for _, opt := range opts {
		err := opt(cfg)
		require.NoError(t, err)
	}

	// Verify
	assert.Equal(t, "test-container", cfg.name)
	assert.Equal(t, "14", cfg.tag)
	assert.Equal(t, []string{"FOO=bar", "BAZ=qux"}, cfg.env)
	assert.Equal(t, []string{"echo", "hello"}, cfg.cmd)
	assert.True(t, cfg.privileged)
	assert.Equal(t, "linux/amd64", cfg.platform)
	assert.True(t, cfg.tty)
	assert.Equal(t, "custom-id", cfg.reuseID)
	assert.True(t, cfg.noReuse)
}

func TestWithPortBindings(t *testing.T) {
	cfg := &runConfig{}

	portMap := nat.PortMap{
		"5432/tcp": []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: "5432"},
		},
	}

	err := WithPortBindings(portMap)(cfg)
	require.NoError(t, err)
	assert.Equal(t, portMap, cfg.portBindings)
}

func TestWithMounts(t *testing.T) {
	cfg := &runConfig{}

	mounts := []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: "/host/path",
			Target: "/container/path",
		},
	}

	err := WithMounts(mounts)(cfg)
	require.NoError(t, err)
	assert.Equal(t, mounts, cfg.mounts)
}

func TestWithLabels(t *testing.T) {
	cfg := &runConfig{}

	labels := map[string]string{
		"app":     "test",
		"version": "1.0",
	}

	err := WithLabels(labels)(cfg)
	require.NoError(t, err)
	assert.Equal(t, labels, cfg.labels)
}

func TestDefaultTag(t *testing.T) {
	cfg := newRunConfig()
	// Default tag should be "latest"
	assert.Equal(t, "latest", cfg.tag)
}
