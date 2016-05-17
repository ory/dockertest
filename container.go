package dockertest

import (
	"fmt"
	"time"
)

// ContainerID represents a container and offers methods like Kill or IP.
type ContainerID string

// Kill runs "docker kill" on the container.
func (c ContainerID) Kill() error {
	return KillContainer(string(c))
}

// Remove runs "docker rm" on the container
func (c ContainerID) Remove() error {
	if Debug || c == "nil" {
		return nil
	}
	return runDockerCommand("docker", "rm", "-v", string(c)).Run()
}

// KillRemove calls Kill on the container, and then Remove if there was
// no error.
func (c ContainerID) KillRemove() error {
	if err := c.Kill(); err != nil {
		return err
	}
	return c.Remove()
}

// lookup retrieves the ip address of the container, and tries to reach
// before timeout the tcp address at this ip and given port.
func (c ContainerID) lookup(timeout time.Duration) (ip string, err error) {
	portMap, err := ports(string(c))
	if err != nil {
		err = fmt.Errorf("error reading Ports: %v", err)
		return
	}

	err = portMap.Wait(timeout)

	// Extract some IP
	for _, v := range portMap {
		ip = v.Host
		break
	}
	return
}
