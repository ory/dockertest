---
date: 2026-02-24
reason: v3-baseline comparison of dockertest v3 vs v4 (refactor-to-v4 branch)
---

# v3 Baseline Comparison: dockertest v3 vs v4

This report uses v3 as the baseline and documents what v4 changes, adds, removes,
and breaks relative to v3.

## 1. Module and Dependencies

### v3 (baseline)
- Module: `github.com/ory/dockertest/v3`
- Go version: 1.22
- Docker client: `github.com/ory/dockertest/v3/docker` (vendored fork of `github.com/fsouza/go-dockerclient`)
- Backoff: `github.com/cenkalti/backoff/v4`
- Heavy dependency tree: moby/term, containerd/continuity, opencontainers/runc, sirupsen/logrus, docker/cli, Nvveen/Gotty

### v4 (changes)
- Module: `github.com/ory/dockertest/v4`
- Go version: 1.25.5
- Docker client: `github.com/moby/moby/client` (official Moby client) via `internal/client.DockerClient` interface
- Backoff: `github.com/cenkalti/backoff/v5`
- Significantly smaller dependency tree: drops logrus, runc, continuity, Gotty, docker/cli, the entire vendored docker package

**Impact**: v4 eliminates the large vendored `docker/` subpackage (~30+ files including client.go, container.go, image.go, network.go, etc.) and replaces it with the official Moby client. This is a major dependency reduction.

## 2. Package Structure

### v3 (baseline)
- Single file: `dockertest.go` (~520 lines) contains Pool, Resource, Network, RunOptions, BuildOptions, ExecOptions, and all methods
- Subpackage: `docker/` (vendored go-dockerclient with types, client, opts)
- Subpackage: `docker/opts/` (mount parser, default host)
- Subpackage: `docker/pkg/` (various utilities)
- Subpackage: `docker/types/` (type definitions)

### v4 (changes)
- Split into multiple files:
  - `pool.go` - Pool struct, NewPool, NewPoolT, Run, RunT, Close, cleanup, resource tracking
  - `resource.go` - Resource methods: GetPort, GetBoundIP, GetHostPort, Close, CloseT, Cleanup, Logs, Exec
  - `network.go` - Network struct, CreateNetwork, CreateNetworkT, Close, CloseT, ConnectToNetwork, DisconnectFromNetwork, GetIPInNetwork
  - `options.go` - PoolOption, RunOption, runConfig, all With* option functions
  - `build.go` - BuildOptions, BuildAndRun, BuildAndRunT, build context creation, stream draining
  - `registry.go` - Resource struct definition, global registry (Register, Get, GetAll, ResetRegistry), scoped registry internals
  - `retry.go` - Retry, RetryWithBackoff, Pool.Retry
  - `errors.go` - Sentinel errors (ErrImagePullFailed, ErrContainerCreateFailed, ErrContainerStartFailed, ErrClientClosed)
  - `doc.go` - Package documentation
  - `internal/client/interface.go` - DockerClient interface
  - `internal/client/moby.go` - NewMobyClient factory
- No subpackages exposed to users (internal only)

**Impact**: Better code organization. The monolithic `dockertest.go` is gone. The `docker/` vendor tree is entirely removed.

## 3. API Surface Changes

### 3.1 Pool Creation

#### v3 (baseline)
```go
func NewPool(endpoint string) (*Pool, error)
func NewTLSPool(endpoint, certpath string) (*Pool, error)
```
- `endpoint` accepts docker host URL, empty string triggers auto-detection (env vars, docker-machine, platform defaults)
- `Pool.Client` is exported (`*dc.Client`)
- `Pool.MaxWait` exported field

#### v4 (changes)
```go
func NewPool(ctx context.Context, endpoint string, opts ...PoolOption) (*Pool, error)
func NewPoolT(t *testing.T, endpoint string, opts ...PoolOption) *Pool
```
- `endpoint` MUST be empty string; non-empty returns error (use `DOCKER_HOST` env var or `WithMobyClient` option)
- `NewTLSPool` removed entirely (TLS handled by moby client via env vars)
- `Pool.client` is **unexported** (`client.DockerClient` interface)
- `Pool.Client` field no longer exists - users cannot access the Docker client directly
- New: `NewPoolT` test helper with `t.Context()` and `t.Cleanup()`
- New: `Pool.Close(ctx)` method (v3 had no Pool.Close)
- New: functional options pattern (`WithMaxWait`, `WithMobyClient`)
- New: `Pool.ownedClient` tracks client ownership for proper cleanup

**Breaking**: No direct client access. Users who called `pool.Client.Ping()`, `pool.Client.InspectContainer()`, etc. in v3 cannot do so in v4.

### 3.2 Running Containers

#### v3 (baseline)
```go
func (d *Pool) Run(repository, tag string, env []string) (*Resource, error)
func (d *Pool) RunWithOptions(opts *RunOptions, hcOpts ...func(*dc.HostConfig)) (*Resource, error)
```
- `RunOptions` struct with 22 fields (Hostname, Name, Repository, Tag, Env, Entrypoint, Cmd, Mounts, Links, ExposedPorts, ExtraHosts, CapAdd, SecurityOpt, DNS, WorkingDir, NetworkID, Networks, Labels, Auth, PortBindings, Privileged, User, Tty, Platform)
- Host config modifier via variadic `func(*dc.HostConfig)` callbacks
- Image pull uses vendored client with auth configuration and creds helpers
- Retry logic for inspecting container port bindings (`inspectContainerWithRetries`, 10 retries with 100ms sleep)

#### v4 (changes)
```go
func (p *Pool) Run(ctx context.Context, repository string, opts ...RunOption) (*Resource, error)
func (p *Pool) RunT(t TestingTB, repository string, opts ...RunOption) *Resource
```
- Functional options pattern replaces `RunOptions` struct:
  - `WithTag(tag)`, `WithEnv(env)`, `WithCmd(cmd)`, `WithEntrypoint(ep)`, `WithUser(user)`, `WithWorkingDir(dir)`, `WithLabels(labels)`, `WithHostname(hostname)`
  - `WithContainerConfig(func(*container.Config))` - escape hatch for any container config
  - `WithHostConfig(func(*container.HostConfig))` - escape hatch for host config
  - `WithReuseID(id)`, `WithoutReuse()` - new container reuse control
- Tag and env are no longer positional params (moved to options)
- `RunT` test helper added

**Removed v3 RunOptions fields with no direct v4 equivalent**:
- `Name` - no `WithName()` option
- `Mounts` - users must use `WithHostConfig` to set `HostConfig.Binds`
- `Links` - no direct option (deprecated Docker feature)
- `ExposedPorts` - users must use `WithContainerConfig` to set `Config.ExposedPorts`
- `ExtraHosts` - users must use `WithHostConfig`
- `CapAdd` - users must use `WithHostConfig`
- `SecurityOpt` - users must use `WithHostConfig`
- `DNS` - users must use `WithHostConfig`
- `NetworkID` - no direct option
- `Networks` - no direct option for attaching at creation time
- `Auth` - no auth configuration for image pulls
- `PortBindings` - users must use `WithHostConfig`
- `Privileged` - users must use `WithHostConfig`
- `Tty` - users must use `WithContainerConfig`
- `Platform` - no platform option

**New in v4**:
- Container reuse system (global registry with scoped lookups, `WithReuseID`, `WithoutReuse`)
- `inspectContainerWithRetries` removed (no port binding retry logic)
- `noPull` option for locally built images

### 3.3 Building Images

#### v3 (baseline)
```go
func (d *Pool) BuildAndRun(name, dockerfilePath string, env []string) (*Resource, error)
func (d *Pool) BuildAndRunWithOptions(dockerfilePath string, opts *RunOptions, hcOpts ...func(*dc.HostConfig)) (*Resource, error)
func (d *Pool) BuildAndRunWithBuildOptions(buildOpts *BuildOptions, runOpts *RunOptions, hcOpts ...func(*dc.HostConfig)) (*Resource, error)
```
- `BuildOptions`: Dockerfile, ContextDir, BuildArgs ([]dc.BuildArg), Platform, Version, Auth
- Three different build entry points
- Build uses vendored client's `BuildImage`

#### v4 (changes)
```go
func (p *Pool) BuildAndRun(ctx context.Context, name string, buildOpts *BuildOptions, runOpts ...RunOption) (*Resource, error)
func (p *Pool) BuildAndRunT(t TestingTB, name string, buildOpts *BuildOptions, runOpts ...RunOption) *Resource
```
- `BuildOptions`: Dockerfile, ContextDir, Tags ([]string), BuildArgs (map[string]*string), Labels, NoCache, ForceRemove
- Single build entry point (simplified from 3)
- Build context created manually via tar archive (custom `createBuildContext` function)
- Build stream consumed and error-checked via `drainBuildStream` using `jsonstream.Message`
- Image cleanup on build failure
- `splitImageReference` uses `github.com/distribution/reference` for proper tag parsing

**Removed**:
- `BuildAndRunWithOptions` - merged into `BuildAndRun`
- `BuildAndRunWithBuildOptions` - merged into `BuildAndRun`
- `BuildOptions.Platform` - no platform support
- `BuildOptions.Version` - no builder version selection (classic vs BuildKit)
- `BuildOptions.Auth` - no auth for builds

**Changed**:
- `BuildArgs` changed from `[]dc.BuildArg` (Name/Value struct) to `map[string]*string` (moby API format)
- `Tags` replaces implicit image naming

### 3.4 Resource (Container) Management

#### v3 (baseline)
```go
type Resource struct {
    pool      *Pool
    Container *dc.Container  // pointer to vendored Container type
}
func (r *Resource) GetPort(id string) string
func (r *Resource) GetBoundIP(id string) string
func (r *Resource) GetHostPort(portID string) string
func (r *Resource) Exec(cmd []string, opts ExecOptions) (exitCode int, err error)
func (r *Resource) GetIPInNetwork(network *Network) string
func (r *Resource) ConnectToNetwork(network *Network) error
func (r *Resource) DisconnectFromNetwork(network *Network) error
func (r *Resource) Close() error
func (r *Resource) Expire(seconds uint) error
```
- `Resource.Container` is `*dc.Container` (pointer, vendored type)
- `ExecOptions` struct with Env, StdIn, StdOut, StdErr, TTY fields
- `Exec` returns `(exitCode int, err error)`
- `Expire` stops container after delay via goroutine

#### v4 (changes)
```go
type Resource struct {
    pool      *Pool
    Container container.InspectResponse  // value type, official moby type
    reuseID   string                     // new: tracks reuse identity
}
func (r *Resource) ID() string                                                    // new
func (r *Resource) GetPort(portID string) string
func (r *Resource) GetBoundIP(portID string) string
func (r *Resource) GetHostPort(portID string) string
func (r *Resource) Close(ctx context.Context) error
func (r *Resource) CloseT(t TestingTB)                                            // new
func (r *Resource) Cleanup(t TestingTB)                                           // new
func (r *Resource) Logs(ctx context.Context) (string, error)                      // new
func (r *Resource) Exec(ctx context.Context, cmd []string) (ExecResult, error)    // changed
func (r *Resource) ConnectToNetwork(ctx context.Context, net *Network) error
func (r *Resource) DisconnectFromNetwork(ctx context.Context, net *Network) error
func (r *Resource) GetIPInNetwork(net *Network) string
```

**Changed**:
- `Container` field changed from `*dc.Container` (pointer) to `container.InspectResponse` (value type)
- `Close()` now takes `context.Context` and calls both stop + remove (v3 only called `Purge` = remove)
- `Exec` signature changed: takes `context.Context`, no `ExecOptions` struct, returns `ExecResult` struct with StdOut/StdErr/ExitCode
- `ConnectToNetwork` and `DisconnectFromNetwork` now take `context.Context`
- Port methods use `network.ParsePort()` instead of raw string casting to `dc.Port`
- `GetBoundIP` handles IPv6 (checks `"::"` in addition to `"0.0.0.0"`)
- `Close` checks for `ErrClientClosed` and tolerates already-removed containers

**Removed**:
- `Expire(seconds uint) error` - no equivalent in v4
- `ExecOptions` struct (Env, StdIn, StdOut, StdErr, TTY fields all gone)
- Exec no longer supports custom env vars, stdin attachment, or TTY mode

**New**:
- `ID()` convenience method
- `CloseT(t)` test helper
- `Cleanup(t)` registers cleanup with `t.Cleanup()`
- `Logs(ctx)` retrieves container logs with Docker stream demultiplexing
- `ExecResult` struct (StdOut, StdErr, ExitCode as strings/int)
- `reuseID` field for container reuse tracking

### 3.5 Network Management

#### v3 (baseline)
```go
type Network struct {
    pool    *Pool
    Network *dc.Network  // pointer to vendored Network type
}
func (d *Pool) CreateNetwork(name string, opts ...func(config *dc.CreateNetworkOptions)) (*Network, error)
func (d *Pool) NetworksByName(name string) ([]Network, error)
func (d *Pool) RemoveNetwork(network *Network) error
func (n *Network) Close() error
```

#### v4 (changes)
```go
type Network struct {
    pool    *Pool
    Network network.Inspect  // value type, official moby type
}
type NetworkCreateOptions struct { Driver, Labels, Options, Internal, Attachable, Ingress, EnableIPv6 }
func (p *Pool) CreateNetwork(ctx context.Context, name string, opts *NetworkCreateOptions) (*Network, error)
func (p *Pool) CreateNetworkT(t TestingTB, name string, opts *NetworkCreateOptions) *Network
func (n *Network) Close(ctx context.Context) error
func (n *Network) CloseT(t TestingTB)
```

**Changed**:
- `Network.Network` changed from `*dc.Network` to `network.Inspect` (value type)
- `CreateNetwork` takes `context.Context` and a typed `NetworkCreateOptions` struct (replaces variadic `func(*dc.CreateNetworkOptions)`)
- `Close` takes `context.Context`
- Network creation includes automatic retry with custom subnet when default pool is exhausted (`retryNetworkCreateWithCustomSubnet`)
- Networks are tracked by Pool for automatic cleanup

**Removed**:
- `Pool.NetworksByName(name)` - no equivalent
- `Pool.RemoveNetwork(network)` - replaced by `Network.Close(ctx)`
- `RemoveNetwork`'s behavior of disconnecting all containers before removing - v4's `Network.Close` does not disconnect containers first

**New**:
- `NetworkCreateOptions` struct with typed fields
- `CreateNetworkT` test helper
- `CloseT` test helper
- `retryNetworkCreateWithCustomSubnet` - automatic fallback when Docker's subnet pool is exhausted
- Pool-level network tracking (`trackNetwork`/`untrackNetwork`)

### 3.6 Pool Utility Methods

#### v3 (baseline)
```go
func (d *Pool) Retry(op func() error) error
func (d *Pool) Purge(r *Resource) error
func (d *Pool) ContainerByName(containerName string) (*Resource, bool)
func (d *Pool) RemoveContainerByName(containerName string) error
func (d *Pool) CurrentContainer() (*Resource, error)
```
- `Retry` uses exponential backoff with `MaxWait` as timeout, 5s max interval
- `Purge` removes container with force + volume removal

#### v4 (changes)
```go
func (p *Pool) Retry(ctx context.Context, timeout time.Duration, fn func() error) error
func Retry(ctx context.Context, timeout, interval time.Duration, fn func() error) error          // package-level
func RetryWithBackoff(ctx context.Context, timeout, initialInterval, maxInterval time.Duration, fn func() error) error  // package-level
```

**Changed**:
- `Pool.Retry` now takes `context.Context` and explicit `timeout` parameter
- Uses constant backoff (1 second interval) instead of exponential
- `RandomizationFactor` set to 0 for deterministic behavior

**Removed**:
- `Pool.Purge(r)` - replaced by `Resource.Close(ctx)`
- `Pool.ContainerByName(name)` - no equivalent
- `Pool.RemoveContainerByName(name)` - no equivalent
- `Pool.CurrentContainer()` - no equivalent
- `ErrNotInContainer` sentinel error

**New**:
- Package-level `Retry` function (not tied to Pool)
- `RetryWithBackoff` with configurable initial/max intervals
- Deterministic backoff (no jitter)

## 4. Context Usage

### v3 (baseline)
- **No `context.Context` anywhere**. All operations are blocking with no cancellation support.
- Timeouts handled by `MaxWait` field on Pool and `backoff.MaxElapsedTime`.
- `Expire` uses a goroutine with `StopContainer(id, seconds)`.

### v4 (changes)
- **Every operation takes `context.Context`**: NewPool, Run, BuildAndRun, CreateNetwork, Close, ConnectToNetwork, DisconnectFromNetwork, Exec, Logs, Retry
- `*T` helper methods use `t.Context()` automatically
- `Pool.cleanup` creates a detached context with 60-second timeout for cleanup
- `Resource.CloseT` and `Network.CloseT` use `context.WithoutCancel(t.Context())`

## 5. Error Handling

### v3 (baseline)
- Error wrapping with `fmt.Errorf("message: %w", err)`
- Some error messages start with uppercase ("Create exec failed", "Failed to connect container to network")
- `ErrNotInContainer` sentinel error
- Type assertion for `*dc.NoSuchContainer` error in `CurrentContainer`

### v4 (changes)
- Sentinel errors defined in `errors.go`: `ErrImagePullFailed`, `ErrContainerCreateFailed`, `ErrContainerStartFailed`, `ErrClientClosed`
- Error wrapping uses `%w` with sentinel errors for `errors.Is()` matching
- Uses `errdefs.IsNotFound()` (from containerd) for "not found" checks
- Consistent lowercase error messages
- `ErrClientClosed` checked before operations on Resource and Network

## 6. Container Reuse System (New in v4)

v3 has no container reuse. Every `Run` or `RunWithOptions` call creates a new container.

v4 introduces a global registry system:
- `registry.go` defines `globalRegistry` (`sync.Map`) keyed by `registryKey{scope, reuseID}`
- By default, containers are reused by `repository:tag` key
- `WithReuseID(id)` allows custom reuse keys
- `WithoutReuse()` disables reuse
- Scoped isolation: pools with default client share scope; pools with custom clients get isolated scopes
- Public API: `Register(reuseID, resource)`, `Get(reuseID)`, `GetAll()`, `ResetRegistry()`
- Race-safe: `sync.Map.LoadOrStore` ensures exactly one container per reuse ID
- On `Pool.Close`, the pool's scope is reset via `resetRegistryWithScope`

## 7. Resource Tracking and Cleanup (New in v4)

v3 has no automatic cleanup. Users must call `pool.Purge(resource)` or `resource.Close()` manually.

v4 tracks all resources and networks:
- `Pool.resources` and `Pool.networks` (`sync.Map`)
- `trackResource`/`untrackResource` and `trackNetwork`/`untrackNetwork`
- `Pool.Close(ctx)` calls `cleanup(ctx)` which removes all tracked containers first, then networks
- `Pool.cleanup` uses `context.WithoutCancel` with 60-second timeout
- `NewPoolT` registers `pool.Close` with `t.Cleanup`

## 8. Docker Client Abstraction

### v3 (baseline)
- `Pool.Client` is `*dc.Client` (exported, vendored go-dockerclient)
- Users have full access to the Docker client for any operation
- No interface abstraction

### v4 (changes)
- `Pool.client` is unexported, typed as `client.DockerClient` (interface in `internal/client/`)
- Interface has 20 methods covering containers, exec, images, networks, ping, and close
- `internal/client/moby.go` implements factory: `NewMobyClient(ctx)` creates client from env, pings to verify
- `WithMobyClient(c)` option allows injecting custom client
- Client ownership tracked: Pool closes owned clients, leaves injected clients open

## 9. Testing Patterns

### v3 (baseline)
- Single test file: `dockertest_test.go` (~500 lines)
- Uses `TestMain` with global `pool` variable
- Uses `testify/assert` and `testify/require`
- Cleanup via `defer resource.Close()` or `defer pool.Purge(resource)`
- No subtests structure (flat test functions)
- Tests use external database drivers (go-sql-driver/mysql, lib/pq)

### v4 (changes)
- Multiple test files: `pool_test.go`, `resource_test.go`, `network_test.go`, `run_test.go`, `build_test.go`, `retry_test.go`, `registry_test.go`
- No global state - each test creates its own pool via `NewPoolT(t, "")`
- Uses stdlib `testing` only (no testify)
- Cleanup via `t.Cleanup(func() { resource.Close(t.Context()) })` or `resource.CloseT(t)`
- Heavy use of subtests (`t.Run`)
- `testing.Short()` skip for integration tests
- `ResetRegistry()` called in test setup/cleanup to isolate state
- Example tests in `examples/` directory (postgres, mysql, mongodb, cockroachdb, redis)
- Unit tests for Resource methods use constructed types (no Docker required)

## 10. Removed Functionality (Present in v3, Missing in v4)

1. **`NewTLSPool(endpoint, certpath)`** - TLS configuration via explicit cert paths
2. **`Pool.Client` (exported)** - direct Docker client access
3. **`RunOptions.Name`** - naming containers at creation
4. **`RunOptions.Mounts`** with mount parsing (`docker/opts/MountParser`)
5. **`RunOptions.Links`** - container linking
6. **`RunOptions.ExposedPorts`** - explicit port exposure (first-class option)
7. **`RunOptions.ExtraHosts`** - /etc/hosts entries
8. **`RunOptions.CapAdd`** - Linux capabilities
9. **`RunOptions.SecurityOpt`** - security options
10. **`RunOptions.DNS`** - DNS servers
11. **`RunOptions.NetworkID`** - attach to network at creation
12. **`RunOptions.Networks`** - attach to multiple networks at creation
13. **`RunOptions.Auth`** - registry authentication for pulls
14. **`RunOptions.PortBindings`** - explicit port bindings (first-class option)
15. **`RunOptions.Privileged`** - privileged mode
16. **`RunOptions.Tty`** - TTY allocation
17. **`RunOptions.Platform`** - platform selection
18. **`Resource.Expire(seconds)`** - timed container expiration
19. **`ExecOptions.Env`** - custom env for exec
20. **`ExecOptions.StdIn`** - stdin attachment for exec
21. **`ExecOptions.StdOut` / `StdErr`** - custom io.Writer for exec output
22. **`ExecOptions.TTY`** - TTY for exec
23. **`Pool.Purge(r)`** - explicit container removal
24. **`Pool.ContainerByName(name)`** - find container by name
25. **`Pool.RemoveContainerByName(name)`** - remove container by name
26. **`Pool.CurrentContainer()`** - detect running inside container
27. **`Pool.NetworksByName(name)`** - list networks by name
28. **`Pool.RemoveNetwork(network)`** - explicit network removal (with auto-disconnect)
29. **`BuildOptions.Platform`** - build platform selection
30. **`BuildOptions.Version`** - builder version (classic vs BuildKit)
31. **`BuildOptions.Auth`** - registry auth for builds

Items 4-17 and 19-22 are **partially available** via the `WithContainerConfig` and `WithHostConfig` escape hatches, but require users to know the moby API types directly rather than using simple fields.

## 11. New Functionality (Not in v3, Added in v4)

1. **Container reuse** - global registry, `WithReuseID`, `WithoutReuse`, scoped isolation
2. **Automatic cleanup** - Pool tracks and cleans up all resources/networks on Close
3. **`*T` test helpers** - `NewPoolT`, `RunT`, `BuildAndRunT`, `CreateNetworkT`, `CloseT`, `Cleanup`
4. **`TestingTB` interface** - abstraction over `testing.TB` for dependency injection
5. **`Resource.Logs(ctx)`** - container log retrieval with Docker stream demultiplexing
6. **`ExecResult` struct** - structured exec output (StdOut, StdErr, ExitCode)
7. **`Resource.ID()` method** - convenience accessor
8. **Package-level `Retry` and `RetryWithBackoff`** - not tied to Pool
9. **Deterministic retry** - no jitter/randomization
10. **`NetworkCreateOptions` typed struct** - replaces callback-based configuration
11. **Automatic network subnet retry** - handles exhausted subnet pools
12. **Client ownership model** - Pool tracks whether it owns the client
13. **`ErrClientClosed` guard** - operations check for closed client
14. **Sentinel errors** - `ErrImagePullFailed`, `ErrContainerCreateFailed`, `ErrContainerStartFailed`
15. **`internal/client.DockerClient` interface** - testability abstraction
16. **`doc.go` with Quick Start example**

## 12. Summary

v4 is a ground-up rewrite that:

**Improves**:
- Context propagation (every operation is cancellable)
- Code organization (multi-file, separated concerns)
- Dependency footprint (drops vendored docker client for official moby client)
- Test isolation (no global state, per-test pool creation)
- Test speed (container reuse by default)
- Cleanup safety (automatic resource tracking and cleanup)
- Testability (DockerClient interface allows mocking)

**Removes**:
- Direct client access (`Pool.Client`)
- Many convenience fields on `RunOptions` (users must use escape hatches)
- Several utility methods (`ContainerByName`, `RemoveContainerByName`, `CurrentContainer`, `NetworksByName`)
- `Expire` functionality
- Exec customization (env, stdin, stdout/stderr writers, TTY)
- Platform and auth support for builds/pulls
- `NewTLSPool` explicit TLS configuration

**Adds**:
- Functional options pattern
- Container reuse system
- Automatic cleanup
- Test helper methods (`*T` variants)
- Container log retrieval
- Structured exec results
- Deterministic retry
- Network subnet exhaustion handling

The migration cost from v3 to v4 is significant: every call site needs `context.Context`, `RunOptions` struct users need to switch to functional options, and users relying on removed features (direct client access, exec customization, container naming, auth) will need workarounds or feature additions.
