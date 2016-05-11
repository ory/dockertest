package dockertest

import (
	"os/exec"
	"strings"
	"time"
)

// ContainerID represents a container and offers methods like Kill or IP.
type ContainerID string

// Ports retrieves the container's ServicePorts
func (c ContainerID) OrderedPorts(order []int) (ServicePorts, error) {
	ports, err := Ports(string(c))
	if err != nil {
		return ServicePorts{}, err
	}
	return ports.Ordered(order)
}

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
func (c ContainerID) lookup(ports []int, timeout time.Duration) (ServicePorts, error) {
	svcs, err := c.OrderedPorts(ports)
	if err != nil {
		return ServicePorts{}, err
	}

	for i, p := range svcs {
		if p.Host == "0.0.0.0" {
			if DockerMachineAvailable {
				out, err := exec.Command("docker-machine", "ip", DockerMachineName).Output()
				if err != nil {
					return ServicePorts{}, err
				}
				svcs[i].Host = strings.TrimSpace(string(out))
			} else {
				svcs[i].Host = "127.0.0.1"
			}
		}
	}

	return svcs, svcs.Wait(timeout)
}

// internal structure used for describing a running container
type container struct {
	NetworkSettings struct {
		IPAddress string
		Ports     map[string]ServicePorts
	}
}
