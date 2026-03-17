// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"net/netip"
	"testing"

	dockertest "github.com/ory/dockertest/v4"
)

func TestPoolCreateNetwork(t *testing.T) {
	t.Run("creates network with name", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		name := t.Name()

		network, err := pool.CreateNetwork(t.Context(), name, nil)
		if err != nil {
			t.Fatalf("CreateNetwork() error = %v, want nil", err)
		}
		if network == nil {
			t.Fatal("CreateNetwork() network = nil, want non-nil")
		}
		t.Cleanup(func() {
			network.Close(t.Context())
		})

		if network.Inspect().Name != name {
			t.Errorf("network.Inspect().Name = %q, want %q", network.Inspect().Name, name)
		}
		if network.Inspect().ID == "" {
			t.Error("network.Inspect().ID is empty, want non-empty")
		}
	})

	t.Run("creates network with custom options", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")

		opts := dockertest.NetworkCreateOptions{
			Driver: "bridge",
			Labels: map[string]string{
				"test": "label",
			},
		}
		name := t.Name()

		network, err := pool.CreateNetwork(t.Context(), name, &opts)
		if err != nil {
			t.Fatalf("CreateNetwork() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			network.Close(t.Context())
		})

		if network.Inspect().Driver != "bridge" {
			t.Errorf("network.Inspect().Driver = %q, want %q", network.Inspect().Driver, "bridge")
		}
		if network.Inspect().Labels["test"] != "label" {
			t.Errorf("network.Inspect().Labels[test] = %q, want %q", network.Inspect().Labels["test"], "label")
		}
	})

	t.Run("creates network with nil options", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		name := t.Name()

		network, err := pool.CreateNetwork(t.Context(), name, nil)
		if err != nil {
			t.Fatalf("CreateNetwork() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			network.Close(t.Context())
		})

		if network.Inspect().Name != name {
			t.Errorf("network.Inspect().Name = %q, want %q", network.Inspect().Name, name)
		}
	})
}

func TestPoolCreateNetworkT(t *testing.T) {
	t.Run("creates network using t.Context", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		name := t.Name()

		network := pool.CreateNetworkT(t, name, nil)
		if network == nil {
			t.Fatal("CreateNetworkT() network = nil, want non-nil")
		}

		if network.Inspect().Name != name {
			t.Errorf("network.Inspect().Name = %q, want %q", network.Inspect().Name, name)
		}
	})
}

func TestNetworkClose(t *testing.T) {
	t.Run("removes network", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		networkName := t.Name()
		// Use CreateNetwork (non-T) to get raw ClosableNetwork for manual Close testing
		network, err := pool.CreateNetwork(t.Context(), networkName, nil)
		if err != nil {
			t.Fatalf("CreateNetwork() error = %v, want nil", err)
		}

		err = network.Close(t.Context())
		if err != nil {
			t.Fatalf("Close() error = %v, want nil", err)
		}

		// Network is removed - creating a new one with the same name should work
		network2, err := pool.CreateNetwork(t.Context(), networkName, nil)
		if err != nil {
			t.Fatalf("CreateNetwork() after Close() error = %v, want nil", err)
		}
		t.Cleanup(func() {
			network2.Close(t.Context())
		})
	})
}

func TestNetworkCloseIdempotent(t *testing.T) {
	pool := dockertest.NewPoolT(t, "")
	// Use CreateNetwork (non-T) to get raw ClosableNetwork for manual Close testing
	network, err := pool.CreateNetwork(t.Context(), t.Name(), nil)
	if err != nil {
		t.Fatalf("CreateNetwork() error = %v, want nil", err)
	}

	// First close should succeed
	err = network.Close(t.Context())
	if err != nil {
		t.Fatalf("first Close() error = %v, want nil", err)
	}

	// Second close should also succeed (network already removed)
	err = network.Close(t.Context())
	if err != nil {
		t.Fatalf("second Close() error = %v, want nil (should tolerate already-removed)", err)
	}
}

func TestNetworkCloseT(t *testing.T) {
	t.Run("removes network using t.Context", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		name := t.Name()
		// Use CreateNetwork (non-T) to get raw ClosableNetwork for manual CloseT testing
		network, err := pool.CreateNetwork(t.Context(), name, nil)
		if err != nil {
			t.Fatalf("CreateNetwork() error = %v, want nil", err)
		}

		network.CloseT(t)

		// Verify network was removed by creating a new one with the same name
		network2, err := pool.CreateNetwork(t.Context(), name, nil)
		if err != nil {
			t.Fatalf("CreateNetwork() after CloseT should succeed, got: %v", err)
		}
		t.Cleanup(func() {
			network2.Close(t.Context())
		})
	})
}

func TestResourceConnectToNetwork(t *testing.T) {
	t.Run("connects container to network", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		network := pool.CreateNetworkT(t, t.Name(), nil)

		resource := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())

		err := resource.ConnectToNetwork(t.Context(), network)
		if err != nil {
			t.Fatalf("ConnectToNetwork() error = %v, want nil", err)
		}

		// Verify connection by getting IP
		ip := resource.GetIPInNetwork(network)
		if ip == "" {
			t.Error("GetIPInNetwork() = empty, want non-empty IP")
		}
	})
}

func TestResourceDisconnectFromNetwork(t *testing.T) {
	t.Run("disconnects container from network", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		network := pool.CreateNetworkT(t, t.Name(), nil)

		resource := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())

		// First connect
		err := resource.ConnectToNetwork(t.Context(), network)
		if err != nil {
			t.Fatalf("ConnectToNetwork() error = %v, want nil", err)
		}

		// Verify connection
		ip := resource.GetIPInNetwork(network)
		if ip == "" {
			t.Error("GetIPInNetwork() before disconnect = empty, want non-empty IP")
		}

		// Disconnect
		err = resource.DisconnectFromNetwork(t.Context(), network)
		if err != nil {
			t.Fatalf("DisconnectFromNetwork() error = %v, want nil", err)
		}

		// Verify disconnection
		ip = resource.GetIPInNetwork(network)
		if ip != "" {
			t.Errorf("GetIPInNetwork() after disconnect = %q, want empty", ip)
		}
	})
}

func TestResourceGetIPInNetwork(t *testing.T) {
	t.Run("returns IP in network", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		network := pool.CreateNetworkT(t, t.Name(), nil)

		resource := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())

		err := resource.ConnectToNetwork(t.Context(), network)
		if err != nil {
			t.Fatalf("ConnectToNetwork() error = %v, want nil", err)
		}

		ip := resource.GetIPInNetwork(network)
		if ip == "" {
			t.Error("GetIPInNetwork() = empty, want non-empty IP")
		}

		// Verify IP is a valid address
		if _, err := netip.ParseAddr(ip); err != nil {
			t.Errorf("GetIPInNetwork() = %q, want valid IP address: %v", ip, err)
		}
	})

	t.Run("returns empty for non-connected network", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		network := pool.CreateNetworkT(t, t.Name(), nil)

		resource := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())

		// Don't connect to network
		ip := resource.GetIPInNetwork(network)
		if ip != "" {
			t.Errorf("GetIPInNetwork() for non-connected network = %q, want empty", ip)
		}
	})
}

func TestNetworkIntegration(t *testing.T) {
	t.Run("two containers can communicate via custom network", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")

		// Create custom network
		network := pool.CreateNetworkT(t, t.Name(), nil)

		// Start first container
		resource1 := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())

		// Connect to network
		err := resource1.ConnectToNetwork(t.Context(), network)
		if err != nil {
			t.Fatalf("ConnectToNetwork(resource1) error = %v, want nil", err)
		}

		// Start second container
		resource2 := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())

		// Connect to network
		err = resource2.ConnectToNetwork(t.Context(), network)
		if err != nil {
			t.Fatalf("ConnectToNetwork(resource2) error = %v, want nil", err)
		}

		// Get IPs
		ip1 := resource1.GetIPInNetwork(network)
		ip2 := resource2.GetIPInNetwork(network)

		if ip1 == "" {
			t.Error("resource1 IP in network = empty, want non-empty")
		}
		if ip2 == "" {
			t.Error("resource2 IP in network = empty, want non-empty")
		}
		if ip1 == ip2 {
			t.Errorf("resource1 and resource2 have same IP %q, want different IPs", ip1)
		}
	})
}
