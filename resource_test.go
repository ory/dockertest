// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"net/netip"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	dockertest "github.com/ory/dockertest/v4"
)

func TestResourceGetPort(t *testing.T) {
	r := dockertest.NewResource(container.InspectResponse{
		ID: "test123",
		NetworkSettings: &container.NetworkSettings{
			Ports: network.PortMap{
				network.MustParsePort("5432/tcp"): []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "54320"}},
			},
		},
	})

	port := r.GetPort("5432/tcp")
	if port != "54320" {
		t.Errorf("GetPort() = %q, want %q", port, "54320")
	}

	// Test non-existent port
	port = r.GetPort("9999/tcp")
	if port != "" {
		t.Errorf("GetPort(nonexistent) = %q, want empty string", port)
	}

	// Test nil NetworkSettings
	rNil := dockertest.NewResource(container.InspectResponse{
		ID: "test456",
	})
	port = rNil.GetPort("5432/tcp")
	if port != "" {
		t.Errorf("GetPort(nil NetworkSettings) = %q, want empty string", port)
	}
}

func TestResourceGetBoundIP(t *testing.T) {
	r := dockertest.NewResource(container.InspectResponse{
		NetworkSettings: &container.NetworkSettings{
			Ports: network.PortMap{
				network.MustParsePort("5432/tcp"): []network.PortBinding{{HostIP: netip.MustParseAddr("192.168.1.100"), HostPort: "54320"}},
			},
		},
	})

	ip := r.GetBoundIP("5432/tcp")
	if ip != "192.168.1.100" {
		t.Errorf("GetBoundIP() = %q, want %q", ip, "192.168.1.100")
	}

	// Test non-existent port
	ip = r.GetBoundIP("9999/tcp")
	if ip != "" {
		t.Errorf("GetBoundIP(nonexistent) = %q, want empty string", ip)
	}

	// Test nil NetworkSettings
	rNil := dockertest.NewResource(container.InspectResponse{})
	ip = rNil.GetBoundIP("5432/tcp")
	if ip != "" {
		t.Errorf("GetBoundIP(nil NetworkSettings) = %q, want empty string", ip)
	}
}

func TestResourceGetBoundIPLocalhostFallback(t *testing.T) {
	r := dockertest.NewResource(container.InspectResponse{
		NetworkSettings: &container.NetworkSettings{
			Ports: network.PortMap{
				network.MustParsePort("5432/tcp"): []network.PortBinding{{HostIP: netip.MustParseAddr("0.0.0.0"), HostPort: "54320"}},
			},
		},
	})

	ip := r.GetBoundIP("5432/tcp")
	if ip != "127.0.0.1" {
		t.Errorf("GetBoundIP() = %q, want %q", ip, "127.0.0.1")
	}
}

func TestResourceGetHostPort(t *testing.T) {
	r := dockertest.NewResource(container.InspectResponse{
		NetworkSettings: &container.NetworkSettings{
			Ports: network.PortMap{
				network.MustParsePort("5432/tcp"): []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "54320"}},
			},
		},
	})

	hostPort := r.GetHostPort("5432/tcp")
	if hostPort != "127.0.0.1:54320" {
		t.Errorf("GetHostPort() = %q, want %q", hostPort, "127.0.0.1:54320")
	}

	// Test non-existent port
	hostPort = r.GetHostPort("9999/tcp")
	if hostPort != "" {
		t.Errorf("GetHostPort(nonexistent) = %q, want empty string", hostPort)
	}

	// Test nil NetworkSettings
	rNil := dockertest.NewResource(container.InspectResponse{})
	hostPort = rNil.GetHostPort("5432/tcp")
	if hostPort != "" {
		t.Errorf("GetHostPort(nil NetworkSettings) = %q, want empty string", hostPort)
	}
}

func TestResourceGetHostPortIPv6(t *testing.T) {
	r := dockertest.NewResource(container.InspectResponse{
		NetworkSettings: &container.NetworkSettings{
			Ports: network.PortMap{
				network.MustParsePort("5432/tcp"): []network.PortBinding{{HostIP: netip.MustParseAddr("::1"), HostPort: "54320"}},
			},
		},
	})

	hostPort := r.GetHostPort("5432/tcp")
	if hostPort != "[::1]:54320" {
		t.Errorf("GetHostPort() = %q, want %q", hostPort, "[::1]:54320")
	}
}
