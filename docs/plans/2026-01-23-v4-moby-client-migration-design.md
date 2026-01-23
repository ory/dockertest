# Dockertest v4: Migration to Moby Client

**Date:** 2026-01-23
**Status:** Design Approved
**Goal:** Migrate from vendored Docker client to github.com/moby/moby/client while modernizing the API

## Executive Summary

This design outlines the migration of dockertest from its vendored Docker client (6000+ lines) to the official Moby client, while creating v4 with modern Go patterns. Key improvements include:

- **Remove CVE vulnerabilities** from vendored dependencies
- **Reduce dependency bloat** by using official lightweight client
- **Modernize API** with context support, functional options, and typed errors
- **Improve test performance** with automatic container reuse (2-3x faster)
- **Keep v3** around

## Background

When dockertest was created, Docker had no official Go client. The library vendored the Docker client, which now:
- Contains 6000+ lines of code
- Creates security vulnerabilities (regular CVEs)
- Bloats dependencies
- Is difficult to maintain

Docker now provides `github.com/moby/moby/client`, a lightweight official client that we should adopt.

## Goals & Non-Goals

### Goals
1. Eliminate vendored Docker client
2. Create v4 with modern Go API patterns
3. Maintain v3 for backward compatibility
4. Improve test performance with container reuse
5. Add context support throughout
6. Provide typed errors for better error handling
7. Convert examples to executable tests

### Non-Goals
1. Breaking v3 (maintain indefinitely with critical fixes)
2. Supporting Docker versions older than 20.10
3. Automated migration tooling (manual migration via guide)
4. Backward compatibility layer in v4

## Architecture

### Client Abstraction Layer

```go
// Internal interface for testability and future flexibility
type dockerClient interface {
    // Container operations
    ContainerCreate(ctx context.Context, config *container.Config,
        hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig,
        platform *specs.Platform, containerName string) (container.CreateResponse, error)
    ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
    ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
    ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
    ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
    ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
    ContainerExecCreate(ctx context.Context, container string, config types.ExecConfig) (types.IDResponse, error)
    ContainerExecStart(ctx context.Context, execID string, config types.ExecStartCheck) error
    ContainerExecInspect(ctx context.Context, execID string) (types.ContainerExecInspect, error)

    // Image operations
    ImagePull(ctx context.Context, refStr string, options types.ImagePullOptions) (io.ReadCloser, error)
    ImageInspectWithRaw(ctx context.Context, imageID string) (types.ImageInspect, []byte, error)
    ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error)

    // Network operations
    NetworkCreate(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error)
    NetworkInspect(ctx context.Context, networkID string, options types.NetworkInspectOptions) (types.NetworkResource, error)
    NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error
    NetworkDisconnect(ctx context.Context, networkID, containerID string, force bool) error
    NetworkRemove(ctx context.Context, networkID string) error
    NetworkList(ctx context.Context, options types.NetworkListOptions) ([]types.NetworkResource, error)

    // System operations
    Ping(ctx context.Context) (types.Ping, error)
    Close() error
}

// Real implementation wraps moby client
type mobyClient struct {
    *client.Client
}
```

**Benefits:**
- Testable with mocked client
- Future-proof for alternative clients
- Clean separation of concerns

### Public API Design

#### Pool Structure

```go
// Pool manages Docker connections and container lifecycle
type Pool struct {
    client     dockerClient
    maxWait    time.Duration
    mobyClient *client.Client // Exposed for advanced users
}

// PoolOption configures a Pool using functional options pattern
type PoolOption func(*Pool) error

func WithMaxWait(d time.Duration) PoolOption
func WithMobyClient(c *client.Client) PoolOption

// NewPool creates a Pool with sensible defaults
func NewPool(endpoint string, opts ...PoolOption) (*Pool, error)
func NewPoolWithContext(ctx context.Context, endpoint string, opts ...PoolOption) (*Pool, error)
```

#### Container Operations with Context

```go
// RunOption configures container creation
type RunOption func(*runConfig) error

// Functional options
func WithName(name string) RunOption
func WithTag(tag string) RunOption
func WithEnv(env []string) RunOption
func WithCmd(cmd []string) RunOption
func WithExposedPorts(ports ...string) RunOption
func WithPortBindings(bindings nat.PortMap) RunOption
func WithMounts(mounts []mount.Mount) RunOption
func WithNetworks(networks ...*Network) RunOption
func WithLabels(labels map[string]string) RunOption
func WithPrivileged(privileged bool) RunOption
func WithPlatform(platform string) RunOption
func WithTTY(tty bool) RunOption

// Reuse and expiry options
func WithReuseID(id string) RunOption
func WithoutReuse() RunOption
func WithExpiry(d time.Duration) RunOption
func WithoutExpiry() RunOption

// Run starts a container with context support
func (p *Pool) Run(ctx context.Context, repository string, opts ...RunOption) (*Resource, error)

// RunT is test helper that uses t.Context() and fails on error
func (p *Pool) RunT(t testing.TB, repository string, opts ...RunOption) *Resource
```

#### Resource Methods

```go
// Resource represents a running container
type Resource struct {
    pool      *Pool
    Container types.ContainerJSON
}

func (r *Resource) GetPort(portID string) string
func (r *Resource) GetBoundIP(portID string) string
func (r *Resource) GetHostPort(portID string) string
func (r *Resource) Exec(ctx context.Context, cmd []string, opts ...ExecOption) (int, error)
func (r *Resource) ExecT(t testing.TB, cmd []string, opts ...ExecOption) int
func (r *Resource) Close(ctx context.Context) error
func (r *Resource) CloseT(t testing.TB)
func (r *Resource) Expire(ctx context.Context, seconds uint) error
func (r *Resource) ConnectToNetwork(ctx context.Context, network *Network) error
func (r *Resource) DisconnectFromNetwork(ctx context.Context, network *Network) error
func (r *Resource) GetIPInNetwork(network *Network) string
func (r *Resource) Cleanup(t testing.TB) // Registers cleanup with t.Cleanup()
```

#### Typed Errors

```go
type ErrorType int

const (
    ErrTypeUnknown ErrorType = iota
    ErrTypeContainerNotFound
    ErrTypeImageNotFound
    ErrTypeNetworkNotFound
    ErrTypeTimeout
    ErrTypeConnectionRefused
    ErrTypeImagePullFailed
    ErrTypeContainerCreateFailed
    ErrTypeContainerStartFailed
    ErrTypeExecFailed
)

type Error struct {
    Type    ErrorType
    Message string
    Cause   error
}

func (e *Error) Error() string
func (e *Error) Unwrap() error
func (e *Error) Is(target error) bool

// Helper functions
func IsNotFound(err error) bool
func IsTimeout(err error) bool
func IsConnectionError(err error) bool
```

## Container Reuse & Global Registry

### Default Reuse Behavior

**Containers are reused by default** to dramatically improve test performance (2-3x faster).

```go
// Package-level registry - auto-initialized
var (
    registry     sync.Map // map[string]*Resource
    allResources sync.Map // map[*Resource]struct{}
)

const DefaultExpiry = 10 * time.Minute

// Cleanup removes all registered containers
func Cleanup() error
```

### Usage Pattern

```go
func TestMain(m *testing.M) {
    defer dockertest.Cleanup()
    code := m.Run()
    os.Exit(code)
}

// Containers automatically reused by repository:tag
func TestUsers(t *testing.T) {
    pool, _ := dockertest.NewPool("")
    db := pool.RunT(t, "postgres", dockertest.WithTag("14"))
    // Reused across all tests requesting postgres:14
}

// Custom reuse ID for multiple containers of same image
func TestReplication(t *testing.T) {
    pool, _ := dockertest.NewPool("")

    primary := pool.RunT(t, "postgres",
        dockertest.WithTag("14"),
        dockertest.WithReuseID("postgres-primary"),
    )

    replica := pool.RunT(t, "postgres",
        dockertest.WithTag("14"),
        dockertest.WithReuseID("postgres-replica"),
    )
}

// Opt-out of reuse for isolated tests
func TestIsolated(t *testing.T) {
    pool, _ := dockertest.NewPool("")
    db := pool.RunT(t, "postgres",
        dockertest.WithTag("14"),
        dockertest.WithoutReuse(),
    )
}
```

### Expiry Behavior

- **Default expiry:** 10 minutes (safety mechanism)
- **On reuse:** Reset to full duration if Docker API supports it
- **Customizable:** `WithExpiry(duration)` or `WithoutExpiry()`
- **Purpose:** Prevent resource leaks if Cleanup() not called

## Migration Strategy

### Repository Structure

```
github.com/ory/dockertest/
├── v3/                    # Existing code (maintenance mode)
│   ├── docker/            # Vendored client
│   ├── dockertest.go
│   └── go.mod             # module: github.com/ory/dockertest/v3
├── v4/                    # New implementation
│   ├── pool.go
│   ├── resource.go
│   ├── options.go
│   ├── errors.go
│   ├── registry.go
│   ├── internal/client/
│   ├── examples/          # Executable tests
│   └── go.mod             # module: github.com/ory/dockertest/v4
└── docs/
    └── migration-v3-to-v4.md
```

### v3 Maintenance Policy

- **Status:** Maintenance mode only
- **Updates:** Critical security fixes and Docker compatibility
- **No new features:** All development in v4
- **Support timeline:** 12 months after v4 release
- **Clear deprecation notice** in README

### Migration Examples

```go
// v3 code
pool, err := dockertest.NewPool("")
pool.MaxWait = time.Minute * 2
resource, err := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})
defer pool.Purge(resource)

// v4 equivalent
pool, err := dockertest.NewPool("",
    dockertest.WithMaxWait(2 * time.Minute),
)
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
// No manual cleanup - automatic via Cleanup()
```

## Example Conversion Strategy

All markdown examples converted to executable Go tests:

```
v4/examples/
├── postgres_test.go      # PostgreSQL examples
├── mysql_test.go         # MySQL examples
├── mongodb_test.go       # MongoDB examples
├── redis_test.go         # Redis examples
├── cassandra_test.go     # Cassandra examples
├── cockroachdb_test.go   # CockroachDB examples
├── kafka_test.go         # Kafka examples
├── minio_test.go         # Minio examples
├── rethinkdb_test.go     # RethinkDB examples
├── build_test.go         # BuildAndRun examples
└── network_test.go       # Network examples
```

Each example test:
- Demonstrates real-world usage
- Verifies functionality against actual services
- Serves as documentation
- Runs in CI to catch regressions

## Implementation Phases

### Phase 1: Foundation & Client Abstraction
- New v4 module initialization
- Client abstraction interface
- Moby client wrapper
- Typed error system
- Unit tests with mocks

**Files:** `internal/client/`, `errors.go`

### Phase 2: Core Pool & Resource API
- Pool struct and creation
- Resource methods
- Functional options
- Registry with reuse logic
- Default expiry
- Comprehensive unit tests

**Files:** `pool.go`, `resource.go`, `options.go`, `registry.go`

### Phase 3: Advanced Features
- Network operations
- BuildAndRun functionality
- Retry with backoff
- Integration tests

**Files:** `network.go`, `build.go`, `retry.go`

### Phase 4: Examples & Documentation
- Convert all examples to tests
- Migration guide
- README updates
- API documentation (with godoc documents, link to pkg.go.dev docs for dockertest)
- Troubleshooting guide
- Godoc comments

**Files:** `examples/`, `docs/`, `README.md`, `**/*.go`

### Phase 5: Integration Testing & CI
- CI pipeline for v4
- Example tests in CI
- Docker version compatibility matrix
- Performance benchmarks
- Memory leak tests

**Files:** `.github/workflows/`

### Phase 6: v3 Maintenance Mode
- Deprecation notice in v3
- PRs unlikely to be accepted to v3

**Files:** `v3/README.md`, `v3/MAINTENANCE.md`

### Phase 7: Release & Communication
- Release notes
- Blog post
- Community announcements
- GitHub Discussions
- Monitor feedback

## Testing Strategy

### Unit Tests
- Mock dockerClient interface
- Test all functional options
- Test registry reuse logic
- Test error type conversions
- Test expiry calculations
- **Target:** >80% coverage

### Integration Tests
- Require real Docker daemon
- Test actual container lifecycle
- Test network operations
- Test image building
- Run in CI on Linux

### Example Tests
- All examples must pass in CI
- Verify against real services
- Ensure best practices
- Run on every commit

### Compatibility Matrix
- **Docker versions:** 29.0+
- **Go versions:** 1.23+
- **Platforms:** Linux; untested: macOS, Windows

## Technical Decisions

### Finalized Decisions

1. **Minimum Go version:** 1.23
   - Rationale: Modern features, iterators, improved generics

2. **Moby client version:** Use latest (not version-locked)
   - Rationale: Stay current with Docker API improvements
   - Risk: API changes (mitigated by abstraction layer)

3. **Backward compatibility:** No v3 wrapper in v4
   - Rationale: Clean break encourages modern patterns
   - Migration path: Manual with guide

4. **Migration tooling:** No automated v3tov4 CLI
   - Rationale: Simple enough for manual migration
   - Alternative: Clear examples and patterns in docs

5. **Registry on context cancellation:** No special handling
   - Rationale: Cleanup() handles all resources at end
   - Behavior: Cancelled contexts don't affect registry

6. **Expiry on reuse:** Reset to full duration
   - Implementation: Use Docker API if supported, otherwise best effort
   - Rationale: Reused containers should have fresh expiry window

### Docker API Methods Used

From v3 analysis, we use these client methods:
- BuildImage, PullImage, InspectImage
- CreateContainer, StartContainer, StopContainer, InspectContainer
- RemoveContainer, ListContainers
- CreateExec, StartExec, InspectExec
- CreateNetwork, NetworkInfo, ConnectNetwork, DisconnectNetwork
- RemoveNetwork, ListNetworks
- Ping

All map cleanly to moby/moby/client API.

## Release Process

### Pre-release Checklist
- [ ] All tests passing
- [ ] Examples working
- [ ] Documentation complete
- [ ] Migration guide reviewed
- [ ] CHANGELOG.md updated
- [ ] Security review (dependency scanning)
- [ ] Performance benchmarks documented
- [ ] Breaking changes clearly documented

### Release Steps
1. No release, only create a PR against branch v3 for now.

## Appendix: API Comparison

### v3 API
```go
pool, err := dockertest.NewPool("")
pool.MaxWait = time.Minute
resource, err := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})
defer pool.Purge(resource)

exitCode, err := resource.Exec([]string{"psql", "-V"}, dockertest.ExecOptions{})
```

### v4 API
```go
pool, err := dockertest.NewPool("", dockertest.WithMaxWait(time.Minute))
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
// Auto-cleanup via Cleanup()

exitCode := resource.ExecT(t, []string{"psql", "-V"})
```

**Key improvements:**
- Context support throughout
- Functional options for clarity
- Test-first helpers (`RunT`, `ExecT`)
- Automatic cleanup
- Container reuse by default
- Type-safe configuration
