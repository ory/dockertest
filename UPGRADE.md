# Migration Guide: v3 to v4

This guide helps you migrate from dockertest v3 to v4.

## Why Migrate?

- **Security**: Removes vendored client with CVE vulnerabilities
- **Performance**: Faster tests with automatic container reuse
- **Modern Go**: Context support, functional options, sentinel errors
- **Lighter**: Official Moby client vs 6000+ lines of vendored code
- **Maintainable**: No vendored dependencies to update manually

## Key Differences

| v3                                                                                  | v4                                                                                                             |
| ----------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `pool, err := dockertest.NewPool("")`                                               | `pool := dockertest.NewPoolT(t, "")`                                                                           |
| `pool.MaxWait = time.Minute`                                                        | `dockertest.WithMaxWait(time.Minute)`                                                                          |
| `resource, err := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})` | `pool.RunT(t, "postgres", dockertest.WithTag("14"), dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}))` |
| `defer pool.Purge(resource)`                                                        | Automatic via `NewPoolT` cleanup, or `pool.Close(ctx)` in `TestMain`                                           |
| `pool.Retry(func() error { ... })`                                                  | `pool.Retry(ctx, timeout, func() error { ... })`                                                               |
| No context support                                                                  | Context throughout                                                                                             |
| No container reuse                                                                  | Automatic container reuse by default                                                                           |
| Generic errors                                                                      | Sentinel errors (`ErrImagePullFailed`) plus context errors (`context.DeadlineExceeded`)                        |

## Breaking Changes

### Container Expiration

The `Expire` method from v3 is removed as it was not working as documented /
intended due to Docker API limitations.

**Workarounds for container cleanup in CI:**

- Use `pool.Close(ctx)` in `TestMain` to remove all tracked containers and
  networks after tests.
- Run `docker container prune -f` as a CI post-step to remove stopped
  containers.
- Use `WithLabels` to tag test containers for targeted cleanup:
  ```go
  resource := pool.RunT(t, "postgres",
      dockertest.WithLabels(map[string]string{"ci-run": os.Getenv("CI_RUN_ID")}),
  )
  ```
  Then in CI:
  `docker container rm $(docker container ls -q --filter label=ci-run=$CI_RUN_ID)`

### Import Path

```diff
-import "github.com/ory/dockertest/v3"
+import "github.com/ory/dockertest/v4"
```

### Pool Creation

v4 offers two pool creation patterns for tests. **Choose A or B per package, do
not mix them** — mixing causes double-close or resource leaks.

**Option A: `NewPoolT` (recommended for most tests — no `TestMain` needed)**

`NewPoolT` registers cleanup automatically via `t.Cleanup`. All tracked
containers and networks are removed when the test finishes. Self-contained: no
`TestMain` needed.

```go
// v4 — cleanup is automatic
pool := dockertest.NewPoolT(t, "",
    dockertest.WithMaxWait(2 * time.Minute),
)
```

**Option B: `NewPool` + `TestMain` (for shared pools — advanced)**

Use this when you want a single pool shared across all tests in a package. You
must call `pool.Close(ctx)` explicitly.

```go
// v4 — shared pool, manual cleanup
var pool dockertest.ClosablePool

func TestMain(m *testing.M) {
    ctx := context.Background()
    var err error
    pool, err = dockertest.NewPool(ctx, "",
        dockertest.WithMaxWait(2 * time.Minute),
    )
    if err != nil {
        panic(err)
    }
    code := m.Run()
    // Close removes all tracked containers/networks and closes the client.
    // Call before os.Exit — deferred functions do not run after os.Exit.
    pool.Close(ctx)
    os.Exit(code)
}
```

**Outside tests:** Use `NewPool` with explicit cleanup.

```go
ctx := context.Background()
pool, err := dockertest.NewPool(ctx, "")
if err != nil {
    panic(err)
}
defer pool.Close(ctx)
```

> [!NOTE]
>
> The `endpoint` parameter must be empty. v3 accepted Docker endpoint strings
> directly; v4 reads the Docker host from environment variables (`DOCKER_HOST`,
> `DOCKER_TLS_VERIFY`, `DOCKER_CERT_PATH`). If you passed a custom endpoint in
> v3, set `DOCKER_HOST` before calling `NewPool`, or provide a pre-configured
> `*client.Client` from `github.com/moby/moby/client` via `WithMobyClient`. When
> using `WithMobyClient`, the `endpoint` parameter is ignored and the pool will
> not close the client on `pool.Close`.

### Running Containers

```go
// v3
resource, err := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})
defer pool.Purge(resource)

// v4 (in tests) — cleanup happens automatically via pool.Close
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)

// v4 (outside tests)
resource, err := pool.Run(ctx, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
defer resource.Close(ctx)
```

### Retry / Health Check

The `pool.Retry` signature changed to require context and an explicit timeout.
It retries with a fixed 1-second interval. For custom intervals, use the
package-level helpers.

```go
// v3 — uses pool.MaxWait implicitly
pool.Retry(func() error {
    return db.Ping()
})

// v4 — explicit context and timeout (pass 0 to use pool.MaxWait)
// Retries every 1 second.
pool.Retry(ctx, 0, func() error {
    return db.Ping()
})

// v4 — explicit timeout
pool.Retry(ctx, 30*time.Second, func() error {
    return db.Ping()
})
```

Package-level retry helpers with custom intervals:

```go
// Fixed-interval retry (custom interval)
dockertest.Retry(ctx, 30*time.Second, time.Second, func() error {
    return db.Ping()
})

// Exponential backoff retry
dockertest.RetryWithBackoff(ctx, 30*time.Second, 100*time.Millisecond, 5*time.Second, func() error {
    return db.Ping()
})
```

### Functional Options

`RunWithOptions` is replaced by functional options:

```go
// v3
pool.RunWithOptions(&dockertest.RunOptions{
    Repository: "postgres",
    Tag:        "14",
    Env:        []string{"POSTGRES_DB=testdb"},
    Cmd:        []string{"postgres", "-c", "log_statement=all"},
})

// v4
pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_DB=testdb"}),
    dockertest.WithCmd([]string{"postgres", "-c", "log_statement=all"}),
)
```

Available options: `WithTag`, `WithEnv`, `WithCmd`, `WithEntrypoint`,
`WithUser`, `WithWorkingDir`, `WithLabels`, `WithHostname`, `WithName`,
`WithMounts`, `WithPortBindings`, `WithoutReuse`, `WithReuseID`,
`WithContainerConfig`, `WithHostConfig`.

> [!NOTE]
>
> `WithPortBindings` takes `network.PortMap` from
> `github.com/moby/moby/api/types/network`. Similarly, `WithContainerConfig` and
> `WithHostConfig` use types from `github.com/moby/moby/api/types/container`.

### Cleanup Pattern

See [Pool Creation](#pool-creation) for the full `TestMain` pattern.

### Error Handling

Use sentinel errors with `errors.Is()`:

```go
resource, err := pool.Run(ctx, "postgres", dockertest.WithTag("14"))
if errors.Is(err, dockertest.ErrImagePullFailed) {
    // Handle image pull failure
}
if errors.Is(err, context.DeadlineExceeded) {
    // Handle timeout
}
```

Available sentinel errors: `ErrImagePullFailed`, `ErrContainerCreateFailed`,
`ErrContainerStartFailed`, `ErrClientClosed`.

## Migration Steps

1. **Update dependencies:**

   ```bash
   go get github.com/ory/dockertest/v4
   go mod tidy
   ```

2. **Update imports:**

   ```bash
   # macOS
   find . -name "*.go" -exec sed -i '' 's|github.com/ory/dockertest/v3|github.com/ory/dockertest/v4|g' {} \;
   # Linux
   find . -name "*.go" -exec sed -i 's|github.com/ory/dockertest/v3|github.com/ory/dockertest/v4|g' {} \;
   ```

3. **Convert API calls:**

   - `NewPool("")` → `NewPoolT(t, "")` (or `NewPool(ctx, "")` in `TestMain`)
   - `Run()`/`RunWithOptions()` → `RunT(t, ...)` with functional options
   - `pool.Purge(resource)` → automatic via `NewPoolT`, or `pool.Close(ctx)` in
     `TestMain`
   - `pool.Retry(fn)` → `pool.Retry(ctx, timeout, fn)`
   - For non-reused containers: use `WithoutReuse()` (cleanup is automatic with
     `RunT`, or call `resource.Close(ctx)` with `Run`)
   - See [Breaking Changes](#breaking-changes) for full patterns.

4. **Test:** Run `go test ./...` to verify the migration.

## Common Patterns

### Single Container Test

```go
// v3
func TestDB(t *testing.T) {
    pool, _ := dockertest.NewPool("")
    resource, _ := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})
    defer pool.Purge(resource)
    pool.Retry(func() error { return db.Ping() })
    _ = resource.GetPort("5432/tcp")
}

// v4 — NewPoolT handles cleanup automatically
func TestDB(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")
    resource := pool.RunT(t, "postgres",
        dockertest.WithTag("14"),
        dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
    )
    pool.Retry(t.Context(), 30*time.Second, func() error { return db.Ping() })
    _ = resource.GetPort("5432/tcp")
}
```

### Multiple Containers

```go
// v3
pool, _ := dockertest.NewPool("")
db, _ := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})
defer pool.Purge(db)
cache, _ := pool.Run("redis", "7", nil)
defer pool.Purge(cache)

// v4 — NewPoolT handles cleanup automatically
pool := dockertest.NewPoolT(t, "")
db := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
cache := pool.RunT(t, "redis", dockertest.WithTag("7"))
```

### Automatic Container Reuse

> [!WARNING]
>
> Reuse is keyed on `repository:tag` by default. Two calls with the same
> `repo:tag` but **different** env vars, commands, or other options will
> silently return the **same** container. Use `WithReuseID` to distinguish
> containers that share an image but differ in configuration (see
> [Custom Reuse ID](#custom-reuse-id-different-configs) below).

v4 automatically reuses containers with the same `repo:tag` across tests. Each
`NewPoolT` call creates a separate pool, but containers are still shared because
default pools use a common reuse scope:

```go
func TestUser(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")
    db := pool.RunT(t, "postgres", dockertest.WithTag("14")) // Creates container
}

func TestPost(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")
    db := pool.RunT(t, "postgres", dockertest.WithTag("14")) // Reuses same container
}
```

To opt out of reuse for a specific container:

```go
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithoutReuse(),
)
// Cleanup is automatic via RunT
```

### Custom Reuse ID (Different Configs)

```go
// Same image, different configurations using custom reuse IDs
db1 := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_DB=db1"}),
    dockertest.WithReuseID("postgres-db1"),
)

db2 := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_DB=db2"}),
    dockertest.WithReuseID("postgres-db2"),
)
```

### Build and Run Custom Images

The `name` parameter is used as the image tag for the locally built image (e.g.,
`"myapp:test"` tags the image as `myapp:test`). The built image is not pulled
from a registry.

```go
resource, err := pool.BuildAndRun(ctx, "myapp:test",
    &dockertest.BuildOptions{
        ContextDir: "./testdata",
        Dockerfile: "Dockerfile.test",
    },
    dockertest.WithEnv([]string{"APP_ENV=test"}),
)
if err != nil {
    panic(err)
}
defer resource.Close(ctx)

// Or in tests (cleanup is automatic via NewPoolT):
resource := pool.BuildAndRunT(t, "myapp:test",
    &dockertest.BuildOptions{ContextDir: "./testdata"},
)
```

Available `BuildOptions` fields: `ContextDir` (required), `Dockerfile` (defaults
to `"Dockerfile"`), `Tags`, `BuildArgs` (`map[string]*string`), `Labels`,
`NoCache`, `ForceRemove`.

### Container Networks

Networks created with `CreateNetworkT` or `CreateNetwork` are tracked by the
pool and cleaned up when `pool.Close` runs (automatic with `NewPoolT`).

```go
// In tests:
net := pool.CreateNetworkT(t, "test-net", nil)

db := pool.RunT(t, "postgres", dockertest.WithTag("14"))
db.ConnectToNetwork(t.Context(), net)

ip := db.GetIPInNetwork(net) // Get container's IP on this network
db.DisconnectFromNetwork(t.Context(), net)

// Outside tests:
net, err := pool.CreateNetwork(ctx, "test-net", nil)
if err != nil {
    panic(err)
}
defer net.Close(ctx)
```

Available `NetworkCreateOptions` fields: `Driver` (e.g., `"bridge"`,
`"overlay"`), `Labels`, `Options` (driver-specific), `Internal`, `Attachable`,
`Ingress` (swarm mode), `EnableIPv6`.

### Exec and Logs

```go
result, err := resource.Exec(ctx, []string{"pg_isready"})
// result.StdOut, result.StdErr, result.ExitCode

stdout, stderr, err := resource.Logs(ctx)
// stdout and stderr are separated strings

// Stream logs until container exits or ctx is cancelled:
err = resource.FollowLogs(ctx, os.Stdout, os.Stderr)
```

### Advanced: Container Registry

v4 maintains a global in-memory registry for container reuse. You typically do
not need these functions directly — `Pool.Run` and `Pool.RunT` use them
automatically. They are useful for custom cleanup or inspection:

- `Register(reuseID string, r ClosableResource) error` — stores a resource
  (idempotent; keeps existing)
- `Get(reuseID string) (ClosableResource, bool)` — retrieves a resource by reuse
  ID
- `GetAll() []ClosableResource` — returns all registered resources
- `ResetRegistry()` — clears the registry (does **not** stop containers)

### Immediate Cleanup with `CloseT`

Resources and networks have a `CloseT(t)` method that stops/removes immediately
and calls `t.Fatalf` on error. Use this when you need teardown at a specific
point rather than relying on pool-scoped cleanup:

```go
resource, err := pool.Run(t.Context(), "postgres", dockertest.WithTag("14"), dockertest.WithoutReuse())
if err != nil {
    t.Fatal(err)
}
// ... use resource ...
resource.CloseT(t) // immediate removal
```

> [!NOTE]
>
> `CloseT(t)` removes the container **immediately** and calls `t.Fatalf` on
> error. `Cleanup(t)` registers removal via `t.Cleanup` so it runs when the test
> ends. Use `Cleanup(t)` for non-reused containers that should live for the
> test's duration; use `CloseT(t)` when you need teardown at a specific point.

## Troubleshooting

| Issue                      | Solution                                                    |
| -------------------------- | ----------------------------------------------------------- |
| Container not found errors | Use `pool.Close(ctx)` in TestMain instead of manual Purge() |
| Timeout errors             | Increase timeout: `dockertest.WithMaxWait(5 * time.Minute)` |
| Context canceled errors    | Use `t.Context()` or `context.Background()` appropriately   |
| Image pull failures        | Check with `errors.Is(err, dockertest.ErrImagePullFailed)`  |

## Need Help?

- [API Documentation](https://pkg.go.dev/github.com/ory/dockertest/v4)
- [GitHub Issues](https://github.com/ory/dockertest/issues)
- [Examples Directory](examples)

## Migration Checklist

- [ ] Updated go.mod with v4 dependency
- [ ] Updated all imports to v4
- [ ] Converted NewPool to NewPoolT or NewPool with context
- [ ] Converted Run to RunT or Run with context
- [ ] Updated RunWithOptions to use functional options
- [ ] Replaced pool.Purge with pool.Close(ctx) (or NewPoolT auto-cleanup)
- [ ] Updated pool.Retry(fn) to pool.Retry(ctx, timeout, fn)
- [ ] Updated error handling to use errors.Is()
- [ ] Added WithReuseID where same image has different configs
- [ ] Tested all changes
- [ ] Verified container cleanup works correctly
