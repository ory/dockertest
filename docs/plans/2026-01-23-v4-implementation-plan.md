# Dockertest v4: Detailed Implementation Plan

**Date:** 2026-01-23
**Based on:** v4-moby-client-migration-design.md
**Status:** Ready for Implementation

This document breaks down each phase into specific, actionable tasks that can be implemented and tested incrementally.

---

## Phase 1: Foundation & Client Abstraction

**Goal:** Establish v4 module with Docker client abstraction layer

**Duration Estimate:** 3-5 days

### Task 1.1: Initialize v4 Module
- [ ] Create `v4/` directory at repository root
- [ ] Initialize `v4/go.mod` with `module github.com/ory/dockertest/v4`
- [ ] Set `go 1.22` in go.mod
- [ ] Add initial dependencies:
  ```
  github.com/moby/moby (latest)
  github.com/docker/docker (latest)
  github.com/docker/go-connections (latest)
  github.com/cenkalti/backoff/v4 (existing version)
  github.com/opencontainers/image-spec (latest)
  github.com/stretchr/testify (for tests)
  ```
- [ ] Run `go mod tidy`
- [ ] Create basic `.gitignore` for v4 if needed

**Files:** `v4/go.mod`, `v4/go.sum`

### Task 1.2: Define Client Abstraction Interface
- [ ] Create `v4/internal/client/client.go`
- [ ] Define `dockerClient` interface with methods:
  - Container operations: Create, Start, Stop, Inspect, Remove, List
  - Exec operations: ExecCreate, ExecStart, ExecInspect
  - Image operations: Pull, Inspect, Build
  - Network operations: Create, Inspect, Connect, Disconnect, Remove, List
  - System: Ping, Close
- [ ] Add godoc comments for each method
- [ ] Include context.Context as first parameter for all operations

**Files:** `v4/internal/client/client.go`

**Example:**
```go
package client

import (
    "context"
    "io"

    "github.com/docker/docker/api/types"
    "github.com/docker/docker/api/types/container"
    "github.com/docker/docker/api/types/network"
)

// dockerClient defines the interface for Docker operations.
// This abstraction allows testing with mocks and potential future client implementations.
type dockerClient interface {
    // Container operations
    ContainerCreate(ctx context.Context, config *container.Config,
        hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig,
        platform *specs.Platform, containerName string) (container.CreateResponse, error)
    // ... rest of interface
}
```

### Task 1.3: Implement Moby Client Wrapper
- [ ] Create `v4/internal/client/moby.go`
- [ ] Implement `mobyClient` struct wrapping `*client.Client`
- [ ] Implement all `dockerClient` interface methods
- [ ] Add constructor `NewMobyClient(endpoint string, opts ...ClientOption) (dockerClient, error)`
- [ ] Support TLS configuration
- [ ] Support custom HTTP client
- [ ] Add proper error wrapping (prepare for Phase 1.5)

**Files:** `v4/internal/client/moby.go`

**Example:**
```go
package client

import "github.com/docker/docker/client"

type mobyClient struct {
    *client.Client
}

// NewMobyClient creates a new moby client wrapper
func NewMobyClient(endpoint string, opts ...ClientOption) (dockerClient, error) {
    // Implementation
}

// ContainerCreate wraps the moby client method
func (m *mobyClient) ContainerCreate(ctx context.Context, ...) (container.CreateResponse, error) {
    return m.Client.ContainerCreate(ctx, ...)
}
```

### Task 1.4: Create Mock Client for Testing
- [ ] Create `v4/internal/client/mock.go`
- [ ] Implement `mockClient` struct with configurable behavior
- [ ] Add helper methods for common test scenarios:
  - `WithContainerCreate(response, error)`
  - `WithContainerStart(error)`
  - `WithImagePull(error)`
  - etc.
- [ ] Support call counting and verification

**Files:** `v4/internal/client/mock.go`

**Example:**
```go
package client

type mockClient struct {
    containerCreateFunc func(ctx context.Context, ...) (container.CreateResponse, error)
    containerStartFunc  func(ctx context.Context, ...) error
    // ... other fields

    calls map[string]int // Track method calls
}

func NewMockClient() *mockClient {
    return &mockClient{
        calls: make(map[string]int),
    }
}

func (m *mockClient) WithContainerCreate(resp container.CreateResponse, err error) *mockClient {
    m.containerCreateFunc = func(context.Context, ...) (container.CreateResponse, error) {
        return resp, err
    }
    return m
}
```

### Task 1.5: Implement Typed Error System
- [ ] Create `v4/errors.go`
- [ ] Define `ErrorType` constants
- [ ] Implement `Error` struct with Type, Message, Cause
- [ ] Implement `Error()`, `Unwrap()`, `Is()` methods
- [ ] Add helper functions: `IsNotFound()`, `IsTimeout()`, `IsConnectionError()`
- [ ] Create error wrapping utilities for converting moby errors to typed errors
- [ ] Add comprehensive godoc

**Files:** `v4/errors.go`

**Example:**
```go
package dockertest

import "errors"

type ErrorType int

const (
    ErrTypeUnknown ErrorType = iota
    ErrTypeContainerNotFound
    ErrTypeImageNotFound
    // ... more types
)

type Error struct {
    Type    ErrorType
    Message string
    Cause   error
}

func (e *Error) Error() string {
    if e.Cause != nil {
        return fmt.Sprintf("%s: %v", e.Message, e.Cause)
    }
    return e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

func IsNotFound(err error) bool {
    var e *Error
    if errors.As(err, &e) {
        return e.Type == ErrTypeContainerNotFound || e.Type == ErrTypeImageNotFound
    }
    return false
}
```

### Task 1.6: Write Unit Tests
- [ ] Create `v4/internal/client/client_test.go`
- [ ] Test moby client wrapper methods
- [ ] Test mock client functionality
- [ ] Create `v4/errors_test.go`
- [ ] Test error type detection
- [ ] Test error wrapping and unwrapping
- [ ] Test `Is()` and `Unwrap()` behavior
- [ ] Aim for >80% coverage

**Files:** `v4/internal/client/client_test.go`, `v4/errors_test.go`

### Task 1.7: Phase 1 Documentation
- [ ] Add package documentation to `v4/doc.go`
- [ ] Document client abstraction design in godoc
- [ ] Add examples for error handling
- [ ] Create `v4/README.md` with phase 1 status

**Files:** `v4/doc.go`, `v4/README.md`

---

## Phase 2: Core Pool & Resource API

**Goal:** Implement main public API with container lifecycle management

**Duration Estimate:** 5-7 days

### Task 2.1: Define Configuration Structures
- [ ] Create `v4/config.go`
- [ ] Define `runConfig` struct (internal, unexported)
  - Fields: repository, tag, name, env, cmd, entrypoint, exposedPorts, portBindings, mounts, networks, labels, hostname, workingDir, user, privileged, capAdd, securityOpt, extraHosts, platform, auth, tty
  - Reuse fields: reuseID, noReuse
  - Expiry fields: expiry, hasExpiry
- [ ] Define `poolConfig` struct (internal, unexported)
  - Fields: maxWait, endpoint, tlsConfig, httpClient
- [ ] Add validation methods

**Files:** `v4/config.go`

### Task 2.2: Implement Pool Structure
- [ ] Create `v4/pool.go`
- [ ] Define `Pool` struct:
  ```go
  type Pool struct {
      client     dockerClient
      maxWait    time.Duration
      mobyClient *client.Client
  }
  ```
- [ ] Implement `NewPool(endpoint string, opts ...PoolOption) (*Pool, error)`
- [ ] Implement `NewPoolWithContext(ctx context.Context, endpoint string, opts ...PoolOption) (*Pool, error)`
- [ ] Handle endpoint defaults (DOCKER_HOST, DOCKER_URL, platform defaults)
- [ ] Support TLS configuration from environment
- [ ] Implement `Ping(ctx context.Context) error`
- [ ] Implement `Close() error`

**Files:** `v4/pool.go`

### Task 2.3: Implement Pool Options
- [ ] Create `v4/options.go` (pool options section)
- [ ] Define `PoolOption` type
- [ ] Implement pool options:
  - `WithMaxWait(time.Duration) PoolOption`
  - `WithMobyClient(*client.Client) PoolOption`
  - `WithHTTPClient(*http.Client) PoolOption`
  - `WithTLSConfig(*tls.Config) PoolOption`
- [ ] Add godoc for each option

**Files:** `v4/options.go`

### Task 2.4: Implement Run Options
- [ ] Continue in `v4/options.go` (run options section)
- [ ] Define `RunOption` type: `type RunOption func(*runConfig) error`
- [ ] Implement basic run options:
  - `WithName(string) RunOption`
  - `WithTag(string) RunOption`
  - `WithEnv([]string) RunOption`
  - `WithCmd([]string) RunOption`
  - `WithEntrypoint([]string) RunOption`
  - `WithWorkingDir(string) RunOption`
  - `WithUser(string) RunOption`
  - `WithHostname(string) RunOption`
  - `WithTTY(bool) RunOption`
  - `WithPrivileged(bool) RunOption`
  - `WithPlatform(string) RunOption`
- [ ] Implement port/network options:
  - `WithExposedPorts(...string) RunOption`
  - `WithPortBindings(nat.PortMap) RunOption`
  - `WithNetworks(...*Network) RunOption`
- [ ] Implement mount options:
  - `WithMounts([]mount.Mount) RunOption`
  - `WithBindMount(source, target string) RunOption` (helper)
  - `WithVolumeMount(name, target string) RunOption` (helper)
- [ ] Implement metadata options:
  - `WithLabels(map[string]string) RunOption`
  - `WithCapAdd([]string) RunOption`
  - `WithSecurityOpt([]string) RunOption`
  - `WithExtraHosts([]string) RunOption`
- [ ] Implement auth option:
  - `WithAuth(registry.AuthConfig) RunOption`

**Files:** `v4/options.go`

### Task 2.5: Implement Reuse & Expiry Options
- [ ] Continue in `v4/options.go` (reuse section)
- [ ] Implement reuse options:
  - `WithReuseID(string) RunOption` - custom reuse ID
  - `WithoutReuse() RunOption` - disable reuse
- [ ] Implement expiry options:
  - `WithExpiry(time.Duration) RunOption` - custom expiry
  - `WithoutExpiry() RunOption` - disable expiry
- [ ] Add godoc explaining default behavior
- [ ] Document that default reuse ID is `repository:tag`
- [ ] Document that default expiry is 10 minutes

**Files:** `v4/options.go`

### Task 2.6: Implement Global Registry
- [ ] Create `v4/registry.go`
- [ ] Define package-level variables:
  ```go
  var (
      registry     sync.Map // map[string]*Resource
      allResources sync.Map // map[*Resource]struct{}
  )
  ```
- [ ] Define `DefaultExpiry = 10 * time.Minute`
- [ ] Implement `Cleanup() error`
  - Collect all resources from allResources
  - Clean up in LIFO order
  - Use 30-second timeout per container
  - Clear both maps
  - Return combined errors
- [ ] Add comprehensive godoc explaining reuse behavior

**Files:** `v4/registry.go`

**Example:**
```go
package dockertest

import (
    "context"
    "sync"
    "time"
)

var (
    registry     sync.Map // map[string]*Resource
    allResources sync.Map // map[*Resource]struct{}
)

const DefaultExpiry = 10 * time.Minute

// Cleanup removes all registered containers.
// Call this in TestMain with defer:
//
//   func TestMain(m *testing.M) {
//       defer dockertest.Cleanup()
//       code := m.Run()
//       os.Exit(code)
//   }
func Cleanup() error {
    // Implementation
}
```

### Task 2.7: Implement Pool.Run with Reuse Logic
- [ ] In `v4/pool.go`, implement `Run(ctx context.Context, repository string, opts ...RunOption) (*Resource, error)`
- [ ] Apply all options to runConfig
- [ ] Set default tag to "latest" if not specified
- [ ] Set default expiry if not specified
- [ ] Compute default reuse ID: `fmt.Sprintf("%s:%s", repository, tag)`
- [ ] Implement reuse logic:
  - If not `noReuse`, check registry for existing resource
  - If found, return existing
  - If not found, create new container (call `createContainer`)
  - Register in both registry and allResources
  - Set expiry if configured
- [ ] If `noReuse`, create and only register in allResources
- [ ] Handle image pulling if not present
- [ ] Wrap all errors with typed errors

**Files:** `v4/pool.go`

### Task 2.8: Implement Pool.createContainer Helper
- [ ] In `v4/pool.go`, implement `createContainer(ctx context.Context, cfg *runConfig) (*Resource, error)` (private method)
- [ ] Build `container.Config` from runConfig
- [ ] Build `container.HostConfig` from runConfig
- [ ] Build `network.NetworkingConfig` from runConfig
- [ ] Check if image exists, pull if not
- [ ] Call client.ContainerCreate
- [ ] Call client.ContainerStart
- [ ] Call client.ContainerInspect to get full container info
- [ ] Return &Resource with populated Container field
- [ ] Handle all errors with proper wrapping

**Files:** `v4/pool.go`

### Task 2.9: Implement Pool.RunT Test Helper
- [ ] In `v4/pool.go`, implement `RunT(t testing.TB, repository string, opts ...RunOption) *Resource`
- [ ] Call `t.Helper()`
- [ ] Call `Run(t.Context(), repository, opts...)`
- [ ] Call `t.Fatal(err)` on error
- [ ] Return resource on success

**Files:** `v4/pool.go`

### Task 2.10: Implement Resource Structure
- [ ] Create `v4/resource.go`
- [ ] Define `Resource` struct:
  ```go
  type Resource struct {
      pool      *Pool
      Container types.ContainerJSON
  }
  ```
- [ ] Implement port methods:
  - `GetPort(portID string) string`
  - `GetBoundIP(portID string) string`
  - `GetHostPort(portID string) string`
- [ ] Parse Container.NetworkSettings.Ports correctly
- [ ] Handle "0.0.0.0" -> "localhost" conversion

**Files:** `v4/resource.go`

### Task 2.11: Implement Resource Lifecycle Methods
- [ ] Continue in `v4/resource.go`
- [ ] Implement `Close(ctx context.Context) error`
  - Call pool.client.ContainerRemove with force and remove volumes
  - Wrap errors
- [ ] Implement `CloseT(t testing.TB)`
  - Call t.Helper()
  - Call Close(t.Context())
  - Use t.Error (not Fatal) for cleanup errors
- [ ] Implement `Cleanup(t testing.TB)`
  - Call t.Helper()
  - Register CloseT with t.Cleanup()
- [ ] Implement `Expire(ctx context.Context, seconds uint) error`
  - Call pool.client.ContainerStop with timeout
  - Note: Run in goroutine like v3? Or synchronous? Decision: synchronous for simplicity

**Files:** `v4/resource.go`

### Task 2.12: Implement Resource Exec Methods
- [ ] Continue in `v4/resource.go`
- [ ] Define `ExecOption` type
- [ ] Implement exec options in `v4/options.go`:
  - `WithExecEnv([]string) ExecOption`
  - `WithStdin(io.Reader) ExecOption`
  - `WithStdout(io.Writer) ExecOption`
  - `WithStderr(io.Writer) ExecOption`
  - `WithExecTTY(bool) ExecOption`
- [ ] Implement `Exec(ctx context.Context, cmd []string, opts ...ExecOption) (int, error)`
  - Create exec config
  - Call pool.client.ContainerExecCreate
  - Call pool.client.ContainerExecStart
  - Call pool.client.ContainerExecInspect for exit code
  - Return exit code and error
- [ ] Implement `ExecT(t testing.TB, cmd []string, opts ...ExecOption) int`
  - Call t.Helper()
  - Call Exec(t.Context(), cmd, opts...)
  - Fatal on error
  - Return exit code

**Files:** `v4/resource.go`, `v4/options.go`

### Task 2.13: Implement Resource Network Methods
- [ ] Continue in `v4/resource.go`
- [ ] Implement `ConnectToNetwork(ctx context.Context, network *Network) error`
  - Call pool.client.NetworkConnect
  - Refresh container info via Inspect
  - Refresh network info
  - Wrap errors
- [ ] Implement `DisconnectFromNetwork(ctx context.Context, network *Network) error`
  - Call pool.client.NetworkDisconnect
  - Refresh container and network info
  - Wrap errors
- [ ] Implement `GetIPInNetwork(network *Network) string`
  - Parse Container.NetworkSettings.Networks
  - Return IP for given network

**Files:** `v4/resource.go`

### Task 2.14: Implement Pool.Retry
- [ ] In `v4/pool.go`, implement `Retry(ctx context.Context, op func() error) error`
- [ ] Use cenkalti/backoff/v4
- [ ] Configure exponential backoff (max interval 5s)
- [ ] Use context deadline if set, otherwise pool.maxWait, otherwise 1 minute default
- [ ] Check ctx.Done() in retry loop
- [ ] Return backoff.Permanent on context cancellation
- [ ] Add godoc with example

**Files:** `v4/pool.go`

### Task 2.15: Write Pool Unit Tests
- [ ] Create `v4/pool_test.go`
- [ ] Test NewPool with various endpoints and options
- [ ] Test Run with mock client:
  - Successful container creation
  - Image pull when not present
  - Error handling
- [ ] Test reuse logic:
  - First call creates, second call reuses
  - Different tags create separate containers
  - Custom reuse IDs work correctly
  - WithoutReuse creates new containers
- [ ] Test expiry setting
- [ ] Test Retry with successful and failing operations
- [ ] Test Ping

**Files:** `v4/pool_test.go`

### Task 2.16: Write Resource Unit Tests
- [ ] Create `v4/resource_test.go`
- [ ] Test GetPort, GetBoundIP, GetHostPort with various port configurations
- [ ] Test Close
- [ ] Test Exec with mock client
- [ ] Test Expire
- [ ] Test network methods (ConnectToNetwork, DisconnectFromNetwork, GetIPInNetwork)
- [ ] Use mock Container JSON data

**Files:** `v4/resource_test.go`

### Task 2.17: Write Registry Unit Tests
- [ ] Create `v4/registry_test.go`
- [ ] Test Cleanup removes all resources
- [ ] Test Cleanup clears both maps
- [ ] Test Cleanup handles errors gracefully
- [ ] Test concurrent access to registry
- [ ] Test LIFO cleanup order

**Files:** `v4/registry_test.go`

### Task 2.18: Phase 2 Integration Tests
- [ ] Create `v4/integration_test.go`
- [ ] Skip if Docker not available
- [ ] Test real container lifecycle:
  - Run simple container (alpine or busybox)
  - Execute command
  - Stop container
- [ ] Test reuse behavior with real containers
- [ ] Test Cleanup removes real containers
- [ ] Use build tags: `//go:build integration`

**Files:** `v4/integration_test.go`

### Task 2.19: Phase 2 Documentation
- [ ] Update `v4/README.md` with Phase 2 progress
- [ ] Add godoc examples for Pool.Run, Pool.RunT
- [ ] Add godoc examples for Resource methods
- [ ] Document reuse behavior in package docs

**Files:** `v4/README.md`, `v4/doc.go`

---

## Phase 3: Advanced Features

**Goal:** Add network operations, image building, and advanced features

**Duration Estimate:** 4-6 days

### Task 3.1: Implement Network Structure
- [ ] Create `v4/network.go`
- [ ] Define `Network` struct:
  ```go
  type Network struct {
      pool    *Pool
      network types.NetworkResource
  }
  ```
- [ ] Implement `Close(ctx context.Context) error`
  - Disconnect all containers
  - Remove network
  - Wrap errors
- [ ] Implement `CloseT(t testing.TB)` test helper

**Files:** `v4/network.go`

### Task 3.2: Implement Network Options
- [ ] In `v4/options.go`, add network options section
- [ ] Define `NetworkOption` type
- [ ] Implement network options:
  - `WithDriver(string) NetworkOption`
  - `WithIPAM(*network.IPAM) NetworkOption`
  - `WithInternal(bool) NetworkOption`
  - `WithNetworkLabels(map[string]string) NetworkOption`
  - `WithIPv6(bool) NetworkOption`
  - `WithAttachable(bool) NetworkOption`

**Files:** `v4/options.go`

### Task 3.3: Implement Pool Network Methods
- [ ] In `v4/pool.go`, add network methods
- [ ] Implement `CreateNetwork(ctx context.Context, name string, opts ...NetworkOption) (*Network, error)`
  - Apply options to network config
  - Call client.NetworkCreate
  - Call client.NetworkInspect to get full info
  - Return &Network
- [ ] Implement `CreateNetworkT(t testing.TB, name string, opts ...NetworkOption) *Network`
  - Test helper version
- [ ] Implement `RemoveNetwork(ctx context.Context, network *Network) error`
  - Call network.Close()
- [ ] Implement `NetworksByName(ctx context.Context, name string) ([]*Network, error)`
  - Call client.NetworkList with filters
  - Return list of matching networks

**Files:** `v4/pool.go`

### Task 3.4: Implement Build Options
- [ ] In `v4/options.go`, add build options section
- [ ] Define `BuildOption` type
- [ ] Implement build options:
  - `WithDockerfile(string) BuildOption`
  - `WithContextDir(string) BuildOption`
  - `WithBuildArg(key, value string) BuildOption`
  - `WithBuildPlatform(string) BuildOption`
  - `WithBuildTags(...string) BuildOption`
  - `WithBuildAuth(map[string]registry.AuthConfig) BuildOption`
  - `WithBuildTarget(string) BuildOption` (multi-stage builds)
  - `WithBuildLabels(map[string]string) BuildOption`

**Files:** `v4/options.go`

### Task 3.5: Implement Pool Build Methods
- [ ] Create `v4/build.go`
- [ ] Implement `BuildAndRun(ctx context.Context, name string, buildOpts []BuildOption, runOpts ...RunOption) (*Resource, error)`
  - Apply build options
  - Create tar archive of context directory
  - Call client.ImageBuild
  - Read and discard build output (or optionally log)
  - Set repository to built image name in runConfig
  - Call Run with runOpts
- [ ] Implement `BuildAndRunT(t testing.TB, name string, buildOpts []BuildOption, runOpts ...RunOption) *Resource`
  - Test helper version
- [ ] Handle build errors properly
- [ ] Support Dockerfile in context vs. absolute path

**Files:** `v4/build.go`

### Task 3.6: Implement Helper Methods
- [ ] In `v4/pool.go`, add helper methods
- [ ] Implement `ContainerByName(ctx context.Context, name string) (*Resource, bool)`
  - List containers with name filter
  - Return Resource if found
- [ ] Implement `RemoveContainerByName(ctx context.Context, name string) error`
  - List containers with name filter
  - Remove if found
- [ ] Implement `Purge(ctx context.Context, r *Resource) error`
  - Alias for resource.Close() for v3 compatibility naming

**Files:** `v4/pool.go`

### Task 3.7: Write Network Unit Tests
- [ ] Create `v4/network_test.go`
- [ ] Test CreateNetwork with mock client
- [ ] Test network options application
- [ ] Test Close disconnects and removes
- [ ] Test NetworksByName filtering

**Files:** `v4/network_test.go`

### Task 3.8: Write Build Unit Tests
- [ ] Create `v4/build_test.go`
- [ ] Test BuildAndRun with mock client
- [ ] Test build options application
- [ ] Test Dockerfile parsing and context creation
- [ ] Test error handling

**Files:** `v4/build_test.go`

### Task 3.9: Phase 3 Integration Tests
- [ ] Add to `v4/integration_test.go`
- [ ] Test real network creation and container connection
- [ ] Test BuildAndRun with real Dockerfile
- [ ] Test multi-container networking
- [ ] Test build args and tags

**Files:** `v4/integration_test.go`

### Task 3.10: Phase 3 Documentation
- [ ] Add godoc examples for network operations
- [ ] Add godoc examples for BuildAndRun
- [ ] Update README with advanced features
- [ ] Document common patterns (multi-container setups)

**Files:** `v4/README.md`, `v4/doc.go`, `v4/network.go`, `v4/build.go`

---

## Phase 4: Examples & Documentation

**Goal:** Convert all markdown examples to executable tests and create comprehensive documentation

**Duration Estimate:** 5-7 days

### Task 4.1: Setup Examples Package
- [ ] Create `v4/examples/` directory
- [ ] Create `v4/examples/go.mod` (or use workspace)
- [ ] Create `v4/examples/doc.go` with package documentation
- [ ] Create `v4/examples/helpers.go` with shared utilities:
  - Database connection helpers
  - Common cleanup functions
  - Test data generators
- [ ] Add TestMain to examples (one per file or shared)

**Files:** `v4/examples/doc.go`, `v4/examples/helpers.go`

### Task 4.2: PostgreSQL Examples
- [ ] Create `v4/examples/postgres_test.go`
- [ ] Convert PostgreSQL.md to tests:
  - `TestPostgresBasic` - basic connection and query
  - `TestPostgresWithConfig` - custom configuration
  - `TestPostgresExec` - running commands in container
  - `TestPostgresNetworking` - connecting from another container
  - `TestPostgresWithFixtures` - loading test data
- [ ] Add package docs explaining reuse and cleanup
- [ ] Verify all tests pass with real PostgreSQL

**Files:** `v4/examples/postgres_test.go`

### Task 4.3: MySQL Examples
- [ ] Create `v4/examples/mysql_test.go`
- [ ] Convert MySQL.md to tests:
  - `TestMySQLBasic` - basic connection
  - `TestMySQLWithConfig` - custom my.cnf settings
  - `TestMySQLCharacterSet` - UTF-8 configuration
  - `TestMySQLWithPlatform` - platform-specific images
- [ ] Add package docs
- [ ] Verify tests pass

**Files:** `v4/examples/mysql_test.go`

### Task 4.4: MongoDB Examples
- [ ] Create `v4/examples/mongodb_test.go`
- [ ] Convert MongoDB.md to tests:
  - `TestMongoDBBasic` - basic connection
  - `TestMongoDBWithAuth` - authentication
  - `TestMongoDBReplicaSet` - replica set setup
  - `TestMongoDBCustomPort` - custom port binding
- [ ] Add package docs
- [ ] Verify tests pass

**Files:** `v4/examples/mongodb_test.go`

### Task 4.5: Redis Examples
- [ ] Create `v4/examples/redis_test.go`
- [ ] Convert Redis.md to tests:
  - `TestRedisBasic` - basic connection
  - `TestRedisWithConfig` - custom redis.conf
  - `TestRedisPersistence` - volume mounting
  - `TestRedisCluster` - cluster setup
- [ ] Add package docs
- [ ] Verify tests pass

**Files:** `v4/examples/redis_test.go`

### Task 4.6: Cassandra Examples
- [ ] Create `v4/examples/cassandra_test.go`
- [ ] Convert Cassandra.md to tests:
  - `TestCassandraBasic` - basic connection
  - `TestCassandraWithKeyspace` - keyspace creation
  - `TestCassandraCluster` - multi-node setup
- [ ] Add package docs
- [ ] Verify tests pass

**Files:** `v4/examples/cassandra_test.go`

### Task 4.7: CockroachDB Examples
- [ ] Create `v4/examples/cockroachdb_test.go`
- [ ] Convert CockroachDB.md to tests:
  - `TestCockroachDBBasic` - basic connection
  - `TestCockroachDBInsecure` - insecure mode
  - `TestCockroachDBCluster` - cluster setup
- [ ] Add package docs
- [ ] Verify tests pass

**Files:** `v4/examples/cockroachdb_test.go`

### Task 4.8: Kafka Examples
- [ ] Create `v4/examples/kafka_test.go`
- [ ] Convert Kafka.md to tests:
  - `TestKafkaBasic` - basic producer/consumer
  - `TestKafkaWithZookeeper` - traditional setup
  - `TestKafkaKRaft` - KRaft mode (ZK-less)
- [ ] Add package docs
- [ ] Verify tests pass

**Files:** `v4/examples/kafka_test.go`

### Task 4.9: Minio Examples
- [ ] Create `v4/examples/minio_test.go`
- [ ] Convert Minio.md to tests:
  - `TestMinioBasic` - basic S3 operations
  - `TestMinioBuckets` - bucket operations
  - `TestMinioPresignedURLs` - presigned URL generation
- [ ] Add package docs
- [ ] Verify tests pass

**Files:** `v4/examples/minio_test.go`

### Task 4.10: RethinkDB Examples
- [ ] Create `v4/examples/rethinkdb_test.go`
- [ ] Convert RethinkDB.md to tests:
  - `TestRethinkDBBasic` - basic connection
  - `TestRethinkDBWithDatabase` - database creation
- [ ] Add package docs
- [ ] Verify tests pass

**Files:** `v4/examples/rethinkdb_test.go`

### Task 4.11: Build Examples
- [ ] Create `v4/examples/build_test.go`
- [ ] Convert BuildDockerfile.md to tests:
  - `TestBuildBasic` - simple Dockerfile build
  - `TestBuildWithArgs` - build args
  - `TestBuildMultiStage` - multi-stage builds
  - `TestBuildWithContext` - complex context directory
- [ ] Create test Dockerfiles in `v4/examples/testdata/`
- [ ] Verify tests pass

**Files:** `v4/examples/build_test.go`, `v4/examples/testdata/Dockerfile.*`

### Task 4.12: Network Examples
- [ ] Create `v4/examples/network_test.go`
- [ ] Create networking tests:
  - `TestNetworkBasic` - create network and connect containers
  - `TestNetworkIsolation` - network isolation
  - `TestNetworkMultiple` - container in multiple networks
  - `TestNetworkDNS` - DNS resolution between containers
- [ ] Verify tests pass

**Files:** `v4/examples/network_test.go`

### Task 4.13: Advanced Examples
- [ ] Create `v4/examples/advanced_test.go`
- [ ] Create advanced pattern tests:
  - `TestSharedResources` - reuse pattern
  - `TestParallelTests` - concurrent test execution
  - `TestDatabaseReset` - cleaning between tests
  - `TestMultiServiceSetup` - complex multi-container
  - `TestCustomExpiry` - expiry customization
- [ ] Verify tests pass

**Files:** `v4/examples/advanced_test.go`

### Task 4.14: Create Migration Guide
- [ ] Create `v4/docs/migration-v3-to-v4.md`
- [ ] Section: Why Migrate?
  - Benefits of v4
  - Performance improvements
  - Security improvements
- [ ] Section: Breaking Changes
  - Import path change
  - API signature changes
  - Behavioral changes
- [ ] Section: Migration Patterns
  - Side-by-side comparison for common operations
  - TestMain setup
  - Pool creation
  - Running containers
  - Cleanup patterns
- [ ] Section: New Features
  - Container reuse
  - Context support
  - Typed errors
  - Functional options
- [ ] Section: Common Pitfalls
  - Forgetting Cleanup()
  - Reuse state management
  - Context cancellation behavior

**Files:** `v4/docs/migration-v3-to-v4.md`

### Task 4.15: Create Reuse & Cleanup Guide
- [ ] Create `v4/docs/reuse-and-cleanup.md`
- [ ] Explain reuse behavior in detail
- [ ] Show examples of shared resources
- [ ] Explain database reset patterns
- [ ] Explain cleanup behavior
- [ ] Show debugging techniques
- [ ] Performance tips

**Files:** `v4/docs/reuse-and-cleanup.md`

### Task 4.16: Create Advanced Usage Guide
- [ ] Create `v4/docs/advanced-usage.md`
- [ ] Custom client configuration
- [ ] Network topologies
- [ ] Volume management
- [ ] Build optimization
- [ ] CI/CD integration
- [ ] Debugging techniques
- [ ] Performance tuning

**Files:** `v4/docs/advanced-usage.md`

### Task 4.17: Create Troubleshooting Guide
- [ ] Create `v4/docs/troubleshooting.md`
- [ ] Common errors and solutions
- [ ] Docker connection issues
- [ ] Port conflicts
- [ ] Resource leaks
- [ ] Performance problems
- [ ] Platform-specific issues
- [ ] FAQ section

**Files:** `v4/docs/troubleshooting.md`

### Task 4.18: Update Main README
- [ ] Update `v4/README.md` with complete documentation
- [ ] Quick start guide
- [ ] Installation instructions
- [ ] Basic usage examples
- [ ] Link to examples
- [ ] Link to migration guide
- [ ] Link to other docs
- [ ] Feature highlights
- [ ] Comparison with v3
- [ ] Contributing guidelines

**Files:** `v4/README.md`

### Task 4.19: Generate API Documentation
- [ ] Ensure all public APIs have godoc
- [ ] Add package examples that appear in godoc
- [ ] Verify godoc rendering locally
- [ ] Add badges (build status, coverage, etc.)
- [ ] Test that pkg.go.dev will render correctly

**Files:** All `*.go` files in v4

---

## Phase 5: Integration Testing & CI

**Goal:** Comprehensive testing infrastructure and CI pipeline

**Duration Estimate:** 3-5 days

### Task 5.1: Create CI Workflow for v4 Tests
- [ ] Create `.github/workflows/v4-test.yml`
- [ ] Setup matrix for:
  - Go versions: 1.22, 1.23
  - OS: ubuntu-latest, macos-latest, windows-latest
  - Docker versions: latest, 24.0, 23.0
- [ ] Run unit tests with coverage
- [ ] Run integration tests (with Docker installed)
- [ ] Upload coverage to Codecov
- [ ] Cache Go modules

**Files:** `.github/workflows/v4-test.yml`

### Task 5.2: Create CI Workflow for Examples
- [ ] Create `.github/workflows/v4-examples.yml`
- [ ] Run all example tests
- [ ] Test against real services
- [ ] Set appropriate timeouts (examples may be slow)
- [ ] Run on main branch and PRs
- [ ] Allow manual trigger

**Files:** `.github/workflows/v4-examples.yml`

### Task 5.3: Create Compatibility Test Workflow
- [ ] Create `.github/workflows/v4-compatibility.yml`
- [ ] Test matrix of Docker versions: 20.10, 23.0, 24.0, latest
- [ ] Test with different Docker endpoints (TCP, socket, etc.)
- [ ] Test on multiple platforms
- [ ] Document compatibility results

**Files:** `.github/workflows/v4-compatibility.yml`

### Task 5.4: Add Performance Benchmarks
- [ ] Create `v4/benchmark_test.go`
- [ ] Benchmark container creation (cold start)
- [ ] Benchmark container reuse (warm start)
- [ ] Benchmark parallel container creation
- [ ] Benchmark cleanup performance
- [ ] Compare with v3 benchmarks
- [ ] Document performance improvements

**Files:** `v4/benchmark_test.go`

### Task 5.5: Add Memory Leak Tests
- [ ] Create `v4/leak_test.go`
- [ ] Test registry cleanup completeness
- [ ] Test for goroutine leaks
- [ ] Test for file descriptor leaks
- [ ] Use build tags: `//go:build leak`
- [ ] Run with `-race` flag

**Files:** `v4/leak_test.go`

### Task 5.6: Add Stress Tests
- [ ] Create `v4/stress_test.go`
- [ ] Test with many containers (100+)
- [ ] Test with many parallel creates
- [ ] Test with rapid create/destroy cycles
- [ ] Monitor resource usage
- [ ] Use build tags: `//go:build stress`

**Files:** `v4/stress_test.go`

### Task 5.7: Setup Code Coverage
- [ ] Configure coverage reporting in CI
- [ ] Add coverage badge to README
- [ ] Set minimum coverage threshold (80%)
- [ ] Identify uncovered critical paths
- [ ] Add tests to improve coverage

**Files:** CI workflows, README

### Task 5.8: Add Linting
- [ ] Create `.github/workflows/v4-lint.yml`
- [ ] Run golangci-lint with strict config
- [ ] Check godoc formatting
- [ ] Check for common issues
- [ ] Enforce in PRs

**Files:** `.github/workflows/v4-lint.yml`, `.golangci.yml`

### Task 5.9: Add Security Scanning
- [ ] Add dependency scanning (Dependabot or similar)
- [ ] Add SAST scanning (gosec)
- [ ] Add license checking
- [ ] Document security policy

**Files:** `.github/dependabot.yml`, `.github/workflows/v4-security.yml`

### Task 5.10: Create PR Template
- [ ] Create `.github/PULL_REQUEST_TEMPLATE.md` for v4
- [ ] Checklist for contributors
- [ ] Testing requirements
- [ ] Documentation requirements
- [ ] Link to contributing guide

**Files:** `.github/PULL_REQUEST_TEMPLATE.md`

---

## Phase 6: v3 Maintenance Mode

**Goal:** Prepare v3 for maintenance-only mode

**Duration Estimate:** 1-2 days

### Task 6.1: Add Deprecation Notice to v3
- [ ] Update `v3/README.md` with deprecation notice
- [ ] Add banner at top: "⚠️ v3 is in maintenance mode. Please migrate to v4."
- [ ] Link to v4 repository
- [ ] Link to migration guide
- [ ] Explain support timeline (12 months)

**Files:** `v3/README.md`

### Task 6.2: Create v3 Maintenance Policy
- [ ] Create `v3/MAINTENANCE.md`
- [ ] Document what changes will be accepted:
  - Critical security fixes
  - Docker compatibility fixes
  - No new features
  - No refactoring
- [ ] Document review timeline
- [ ] Document end-of-life date

**Files:** `v3/MAINTENANCE.md`

### Task 6.3: Update v3 CI
- [ ] Create `.github/workflows/v3-critical-only.yml`
- [ ] Minimal testing (basic functionality only)
- [ ] Run on schedule (weekly) to catch Docker breakage
- [ ] No longer run on all PRs

**Files:** `.github/workflows/v3-critical-only.yml`

### Task 6.4: Update Issue Templates
- [ ] Create `.github/ISSUE_TEMPLATE/v3-bug.md`
- [ ] Prompt users to check if fixed in v4
- [ ] Require reproduction steps
- [ ] Clarify maintenance mode

**Files:** `.github/ISSUE_TEMPLATE/v3-bug.md`

### Task 6.5: Update Main README
- [ ] Update repository root `README.md`
- [ ] Highlight v4 as primary version
- [ ] Show v4 examples first
- [ ] Add section for v3 with maintenance notice
- [ ] Update badges for both versions

**Files:** `README.md` (repository root)

---

## Phase 7: Release & Communication

**Goal:** Launch v4 and communicate with community

**Duration Estimate:** 2-3 days

### Task 7.1: Pre-Release Checklist
- [ ] All tests passing (unit, integration, examples)
- [ ] All documentation complete
- [ ] Migration guide reviewed
- [ ] Performance benchmarks run and documented
- [ ] Security scan passing
- [ ] Coverage >80%
- [ ] All examples verified
- [ ] Breaking changes documented
- [ ] CHANGELOG.md created

**Files:** `v4/CHANGELOG.md`

### Task 7.2: Create Release Candidate
- [ ] Tag `v4.0.0-rc.1`
- [ ] Create GitHub release (pre-release)
- [ ] Publish release notes highlighting:
  - Major changes
  - Performance improvements
  - Security improvements
  - Migration path
- [ ] Include link to migration guide
- [ ] Include known issues if any

**Files:** GitHub release

### Task 7.3: Announce Release Candidate
- [ ] Create GitHub Discussion for v4 RC feedback
- [ ] Post in project Slack/Discord (if applicable)
- [ ] Email active contributors
- [ ] Set 2-week feedback window
- [ ] Monitor for issues

**Files:** GitHub Discussion

### Task 7.4: Address RC Feedback
- [ ] Triage issues reported during RC
- [ ] Fix critical bugs
- [ ] Update documentation based on feedback
- [ ] Create RC2 if needed
- [ ] Decide on final release date

**Files:** Bug fixes as needed

### Task 7.5: Create Final Release
- [ ] Tag `v4.0.0`
- [ ] Create GitHub release (final)
- [ ] Publish comprehensive release notes:
  - Executive summary
  - Full changelog
  - Migration guide link
  - Example code snippets
  - Breaking changes
  - New features
  - Performance metrics
  - Contributor thanks
- [ ] Mark as latest release

**Files:** GitHub release, `v4/CHANGELOG.md`

### Task 7.6: Write Blog Post
- [ ] Draft blog post covering:
  - Why we created v4
  - Key improvements
  - Migration story
  - Performance benchmarks
  - Code examples
  - What's next
- [ ] Get feedback from maintainers
- [ ] Publish to project blog/medium

**Files:** Blog post (external)

### Task 7.7: Update Documentation Sites
- [ ] Ensure pkg.go.dev shows v4 docs
- [ ] Update any project documentation sites
- [ ] Update tutorial sites if applicable
- [ ] Update Stack Overflow tag wiki if applicable

**Files:** External documentation

### Task 7.8: Community Announcements
- [ ] Post to Reddit (r/golang)
- [ ] Post to Hacker News (if appropriate)
- [ ] Post to Twitter/X
- [ ] Post to Golang Weekly
- [ ] Post to relevant Go Slack channels
- [ ] Post to LinkedIn (if corporate)

**Files:** Social media posts

### Task 7.9: Monitor Launch
- [ ] Watch GitHub issues for bug reports
- [ ] Monitor GitHub Discussions
- [ ] Track download statistics
- [ ] Respond to community questions
- [ ] Create tracking issue for post-launch improvements
- [ ] Document common issues in FAQ

**Files:** GitHub issues/discussions

### Task 7.10: Plan v4.1 Roadmap
- [ ] Collect community feature requests
- [ ] Prioritize improvements
- [ ] Create milestones for v4.1, v4.2
- [ ] Document roadmap in discussions
- [ ] Start planning next iteration

**Files:** GitHub milestones, roadmap document

---

## Summary

This implementation plan provides concrete, actionable tasks for each phase of the v4 development. Each task should take a few hours to a day, allowing for steady progress and regular commits.

**Total Estimated Duration:** 23-35 days (approximately 5-7 weeks)

**Key Dependencies:**
- Phase 2 requires Phase 1 complete
- Phase 3 requires Phase 2 complete
- Phase 4 can run parallel with Phase 3
- Phase 5 requires Phases 1-4 complete
- Phases 6-7 can run in parallel after Phase 5

**Testing Throughout:**
- Write tests alongside implementation
- Run tests continuously during development
- Integration tests catch issues early
- Examples validate real-world usage

**Regular Checkpoints:**
- End of each phase, review and merge
- Get feedback from other maintainers
- Update design doc if assumptions change
- Celebrate milestones! 🎉
