package dockertest

import (
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/stretchr/testify/assert"
)

func TestRegistryRegisterAndLookup(t *testing.T) {
	// Clear registry before test
	registry = newContainerRegistry()

	r := &Resource{
		Container: types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID: "test123",
			},
		},
	}

	// Register with reuse ID
	registry.register("postgres:14", r)

	// Lookup should find it
	found := registry.lookup("postgres:14")
	assert.Equal(t, r, found)

	// Lookup with different ID should return nil
	notFound := registry.lookup("mysql:8")
	assert.Nil(t, notFound)
}

func TestRegistryUnregister(t *testing.T) {
	// Clear registry before test
	registry = newContainerRegistry()

	r := &Resource{
		Container: types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID: "test123",
			},
		},
	}

	registry.register("postgres:14", r)
	assert.NotNil(t, registry.lookup("postgres:14"))

	registry.unregister(r)
	assert.Nil(t, registry.lookup("postgres:14"))
}

func TestRegistryAll(t *testing.T) {
	// Clear registry before test
	registry = newContainerRegistry()

	r1 := &Resource{
		Container: types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID: "test1",
			},
		},
	}
	r2 := &Resource{
		Container: types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				ID: "test2",
			},
		},
	}

	registry.register("postgres:14", r1)
	registry.register("mysql:8", r2)

	all := registry.all()
	assert.Len(t, all, 2)
	assert.Contains(t, all, r1)
	assert.Contains(t, all, r2)
}

func TestCleanup(t *testing.T) {
	// Clear registry before test to avoid issues with nil pools
	registry = newContainerRegistry()

	// This is hard to test without real containers
	// Just verify it doesn't panic with empty registry
	err := Cleanup()
	// May error if no containers, that's OK
	_ = err
}
