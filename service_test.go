package dockertest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestServicePorts(t *testing.T) {
	assert := assert.New(t)

	assert.Panics(func() { ServicePorts{}.First() })
	assert.Equal(ServicePorts{
		ServicePort{Host: "1"},
		ServicePort{Host: "2"},
	}.First(), ServicePort{Host: "1"})

	assert.NoError(ServicePorts{}.Wait(time.Millisecond * 500))
	assert.Error(ServicePorts{ServicePort{Host: "does-not-exist.tld", Port: "65535"}}.Wait(time.Millisecond * 500))
}

func TestServicePortMap(t *testing.T) {
	assert := assert.New(t)

	{
		servicePorts, err := ServicePortMap{14: ServicePort{Port: "8"}, 8: ServicePort{Port: "8"}}.Ordered([]int{14})
		assert.Nil(err)
		assert.Equal(servicePorts, ServicePorts{ServicePort{Port: "8"}})
	}
	{
		_, err := ServicePortMap{}.Ordered([]int{14})
		assert.Error(err)
	}
}
