package dockertest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFailingContainer(t *testing.T) {
	assert := assert.New(t)

	c := ContainerID("nope")
	assert.Error(c.Kill())
	assert.Error(c.KillRemove())
	assert.Error(c.Remove())
	{
		_, err := c.OrderedPorts([]int{})
		assert.Error(err)
	}
	{
		_, err := c.lookup([]int{}, 5)
		assert.Error(err)
	}
}
