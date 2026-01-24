// Copyright © 2026 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest_test

import (
	"testing"

	dockertest "github.com/ory/dockertest/v4"
)

func TestPoolCreateNetwork(t *testing.T) {
	t.Run("creates network with name", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")

		network, err := pool.CreateNetwork(t.Context(), "test-network", nil)
		if err != nil {
			t.Fatalf("CreateNetwork() error = %v, want nil", err)
		}
		if network == nil {
			t.Fatal("CreateNetwork() network = nil, want non-nil")
		}
		defer network.Close(t.Context())

		if network.Network.Name != "test-network" {
			t.Errorf("network.Network.Name = %q, want %q", network.Network.Name, "test-network")
		}
		if network.Network.ID == "" {
			t.Error("network.Network.ID is empty, want non-empty")
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

		network, err := pool.CreateNetwork(t.Context(), "test-network-opts", &opts)
		if err != nil {
			t.Fatalf("CreateNetwork() error = %v, want nil", err)
		}
		defer network.Close(t.Context())

		if network.Network.Driver != "bridge" {
			t.Errorf("network.Network.Driver = %q, want %q", network.Network.Driver, "bridge")
		}
		if network.Network.Labels["test"] != "label" {
			t.Errorf("network.Network.Labels[test] = %q, want %q", network.Network.Labels["test"], "label")
		}
	})

	t.Run("creates network with nil options", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")

		network, err := pool.CreateNetwork(t.Context(), "test-network-nil", nil)
		if err != nil {
			t.Fatalf("CreateNetwork() error = %v, want nil", err)
		}
		defer network.Close(t.Context())

		if network.Network.Name != "test-network-nil" {
			t.Errorf("network.Network.Name = %q, want %q", network.Network.Name, "test-network-nil")
		}
	})
}

func TestPoolCreateNetworkT(t *testing.T) {
	t.Run("creates network using t.Context", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")

		network := pool.CreateNetworkT(t, "test-network-t", nil)
		if network == nil {
			t.Fatal("CreateNetworkT() network = nil, want non-nil")
		}
		defer network.Close(t.Context())

		if network.Network.Name != "test-network-t" {
			t.Errorf("network.Network.Name = %q, want %q", network.Network.Name, "test-network-t")
		}
	})
}

func TestNetworkClose(t *testing.T) {
	t.Run("removes network", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		// Use a unique name based on current time to avoid conflicts
		networkName := "test-network-close-" + t.Name()
		network := pool.CreateNetworkT(t, networkName, nil)

		err := network.Close(t.Context())
		if err != nil {
			t.Fatalf("Close() error = %v, want nil", err)
		}

		// Network is removed - creating a new one with the same name should work
		network2, err := pool.CreateNetwork(t.Context(), networkName, nil)
		if err != nil {
			t.Fatalf("CreateNetwork() after Close() error = %v, want nil", err)
		}
		defer network2.Close(t.Context())
	})
}

func TestNetworkCloseT(t *testing.T) {
	t.Run("removes network using t.Context", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		network := pool.CreateNetworkT(t, "test-network-closet", nil)

		network.CloseT(t)

		// Network should be removed
	})
}

func TestResourceConnectToNetwork(t *testing.T) {
	t.Run("connects container to network", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		network := pool.CreateNetworkT(t, "test-connect-network", nil)
		defer network.Close(t.Context())

		resource := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())
		defer resource.Close(t.Context())

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
		network := pool.CreateNetworkT(t, "test-disconnect-network", nil)
		defer network.Close(t.Context())

		resource := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())
		defer resource.Close(t.Context())

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
		network := pool.CreateNetworkT(t, "test-getip-network", nil)
		defer network.Close(t.Context())

		resource := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())
		defer resource.Close(t.Context())

		err := resource.ConnectToNetwork(t.Context(), network)
		if err != nil {
			t.Fatalf("ConnectToNetwork() error = %v, want nil", err)
		}

		ip := resource.GetIPInNetwork(network)
		if ip == "" {
			t.Error("GetIPInNetwork() = empty, want non-empty IP")
		}

		// Verify IP format (basic validation)
		if len(ip) < 7 {
			t.Errorf("GetIPInNetwork() = %q, want valid IP address", ip)
		}
	})

	t.Run("returns empty for non-connected network", func(t *testing.T) {
		pool := dockertest.NewPoolT(t, "")
		network := pool.CreateNetworkT(t, "test-noip-network", nil)
		defer network.Close(t.Context())

		resource := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())
		defer resource.Close(t.Context())

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
		network := pool.CreateNetworkT(t, "test-integration-network", nil)
		defer network.Close(t.Context())

		// Start first container
		resource1 := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())
		defer resource1.Close(t.Context())

		// Connect to network
		err := resource1.ConnectToNetwork(t.Context(), network)
		if err != nil {
			t.Fatalf("ConnectToNetwork(resource1) error = %v, want nil", err)
		}

		// Start second container
		resource2 := pool.RunT(t, "alpine", dockertest.WithTag("latest"), dockertest.WithCmd([]string{"sleep", "300"}), dockertest.WithoutReuse())
		defer resource2.Close(t.Context())

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
