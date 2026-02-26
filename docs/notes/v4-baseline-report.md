---
date: 2026-02-24
reason: v4-baseline comparison of dockertest v4 vs v3 (refactor-to-v4 branch)
---

# v4 Baseline Report: dockertest v4 vs v3

This report analyzes v4 as the baseline, evaluating what v4 brings, what v3
lacks, and where v4 may have regressed or overcomplicated things.

## 1. Architecture & Code Organization

### v4 (baseline)

- **12 focused source files** across well-defined concerns: `pool.go`,
  `resource.go`, `network.go`, `registry.go`, `options.go`, `errors.go`,
  `build.go`, `retry.go`, `doc.go`, plus `internal/client/` (interface.go,
  moby.go).
- Clear separation: Pool lifecycle (`pool.go`), container operations
  (`resource.go`), network management (`network.go`), container reuse
  (`registry.go`), option types (`options.go`), build logic (`build.go`).
- Internal client abstraction (`internal/client/interface.go`) enables testing
  via interface injection.

### v3

- **Single file** `dockertest.go` (~530 lines) containing everything: Pool,
  Resource, Network, RunOptions, BuildOptions, all methods.
- Docker client layer is a vendored fork at `docker/` (a copy of
  `github.com/fsouza/go-dockerclient`) — 20+ files including `client.go`,
  `container.go`, `network.go`, `image.go`, `exec.go`, etc.
- No separation of concerns; all types and methods in one file.

### Assessment

v4's modular layout is a clear improvement. Each file has a single
responsibility. The `internal/client` interface decouples the library from
concrete Docker SDK types, enabling unit testing without Docker. v3's monolithic
file made navigation and maintenance harder.

## 2. Docker Client Layer

### v4

- Uses **`github.com/moby/moby/client`** (official Moby SDK, v0.2.1).
- `internal/client.DockerClient` interface (`interface.go:16-44`) abstracts all
  Docker operations with strongly-typed result types per operation (e.g.
  `ContainerCreateResult`, `ImagePullResponse`).
- Client created via `mobyclient.New(mobyclient.FromEnv)` with Ping validation
  (`moby.go:15-27`).
- **5 direct dependencies** in go.mod (backoff, errdefs, reference, moby/api,
  moby/client).

### v3

- Uses a **vendored fork** of `github.com/fsouza/go-dockerclient` (in the
  `docker/` directory). This is a full Docker client implementation with its own
  types (`dc.Container`, `dc.HostConfig`, `dc.Port`, etc.).
- **20+ dependencies** in go.mod including `github.com/docker/docker`,
  `github.com/containerd/continuity`, `github.com/opencontainers/runc`,
  `github.com/sirupsen/logrus`, `github.com/moby/term`, etc.
- No interface abstraction — `Pool.Client` is a concrete `*dc.Client` exposed as
  a public field.

### Assessment

v4's switch to the official Moby SDK is a major improvement:

- Eliminates the massive vendored fork (the `docker/` directory had 20+ files).
- Reduces dependency count from 20+ to 5 direct dependencies.
- Uses the official, maintained SDK instead of a third-party client.
- The `DockerClient` interface enables test doubles without Docker.

**Potential concern:** v3 exposed `Pool.Client` as public, allowing users to
perform arbitrary Docker operations. v4 makes the client private
(`pool.client`). Users needing raw client access must use `WithMobyClient` to
inject one they control. This is better encapsulation but a breaking change for
users who relied on `pool.Client`.

## 3. Context Propagation

### v4

- **Every operation** takes `context.Context` as the first parameter:
  - `NewPool(ctx, endpoint, opts...)` (`pool.go:67`)
  - `Pool.Run(ctx, repository, opts...)` (`pool.go:235`)
  - `Pool.BuildAndRun(ctx, name, buildOpts, runOpts...)` (`build.go:77`)
  - `Pool.CreateNetwork(ctx, name, opts)` (`network.go:51`)
  - `Resource.Close(ctx)` (`resource.go:79`)
  - `Resource.Exec(ctx, cmd)` (`resource.go:193`)
  - `Resource.Logs(ctx)` (`resource.go:125`)
  - `Resource.ConnectToNetwork(ctx, net)` (`network.go:183`)
  - `Pool.Retry(ctx, timeout, fn)` (`retry.go:61`)
- Test helpers use `t.Context()`: `NewPoolT`, `RunT`, `CloseT`, etc.
- Cleanup uses `context.WithoutCancel(ctx)` to ensure cleanup completes even
  when the parent context is cancelled (`pool.go:194`, `resource.go:107`).

### v3

- **No context support at all.** Zero functions accept `context.Context`.
- `NewPool(endpoint)` — no context.
- `Pool.Run(repository, tag, env)` — no context.
- `Resource.Close()` — no context.
- `Pool.Retry(op)` — no context; uses `backoff.MaxElapsedTime` for timeout.

### Assessment

v4's universal context support is a significant improvement. It enables proper
cancellation, timeout propagation, and integration with Go's standard
concurrency patterns. The use of `context.WithoutCancel` in cleanup paths is
particularly thoughtful — it ensures resources are cleaned up even when tests
are cancelled. v3 has no way to cancel long-running operations.

## 4. API Surface & Function Signatures

### v4 Run API

```go
pool.Run(ctx, "postgres", WithTag("14"), WithEnv([]string{"POSTGRES_PASSWORD=secret"}))
```

- Functional options pattern (`RunOption func(*runConfig) error`).
- 12 option functions: `WithTag`, `WithEnv`, `WithCmd`, `WithEntrypoint`,
  `WithReuseID`, `WithoutReuse`, `WithUser`, `WithWorkingDir`, `WithLabels`,
  `WithHostname`, `WithContainerConfig`, `WithHostConfig`.
- Each option returns error for validation.

### v3 Run API

```go
pool.RunWithOptions(&dockertest.RunOptions{Repository: "postgres", Tag: "14", Env: []string{...}})
pool.Run("postgres", "14", []string{...})
```

- Struct-based options (`RunOptions` with 20+ fields).
- `RunWithOptions` takes `*RunOptions` plus variadic `func(*dc.HostConfig)`.
- `Run` is a convenience wrapper with `(repository, tag, env)`.

### Assessment

v4's functional options are more idiomatic Go and more extensible. Adding new
options doesn't break the API. v3's `RunOptions` struct has 20+ fields including
niche ones like `Mounts`, `Links`, `ExtraHosts`, `CapAdd`, `SecurityOpt`, `DNS`,
`Privileged`, `Tty`, `Platform`.

**What v4 drops** compared to v3's RunOptions:

- `Name` (container name) — not directly available; use `WithContainerConfig`.
- `Mounts`, `Links`, `ExtraHosts`, `CapAdd`, `SecurityOpt`, `DNS` — use
  `WithHostConfig` modifier.
- `ExposedPorts` — use `WithContainerConfig` modifier.
- `Privileged`, `Tty` — use `WithHostConfig`/`WithContainerConfig`.
- `Platform` — not available in v4 options.
- `NetworkID`, `Networks` — must connect after creation via
  `Resource.ConnectToNetwork`.
- `PortBindings` — use `WithHostConfig` modifier.
- `Auth` — not available in v4; registry auth is not supported.

The escape hatches `WithContainerConfig` and `WithHostConfig` cover most cases,
but users lose discoverability. In v3, all options were visible struct fields.
In v4, users need to know about the modifier functions for advanced use cases.

## 5. Container Reuse System

### v4

- **Automatic reuse by default** — containers are reused based on
  `repository:tag` (`pool.go:276-284`).
- Global registry with scoped keys (`registry.go`):
  - `registryKey{scope, reuseID}` using `sync.Map`.
  - Default pools share `defaultRegistryScope` (`registry.go:32`).
  - Custom client pools get isolated scopes (`pool.go:96`).
- Options: `WithReuseID(id)` for custom reuse keys, `WithoutReuse()` to disable.
- Registry functions: `Register`, `Get`, `GetAll`, `ResetRegistry`.
- Race-safe: `LoadOrStore` for registration, duplicate containers cleaned up
  (`pool.go:394-408`).

### v3

- **No container reuse.** Every `Run` creates a new container.
- No registry concept at all.
- `ContainerByName` provides name-based lookup but not automatic reuse.

### Assessment

Container reuse is v4's flagship feature. It can make test suites 2-3x faster by
reusing containers across test functions. The scoped registry design prevents
cross-contamination between pools with different clients. The race-safe
`LoadOrStore` pattern correctly handles concurrent test execution.

**Potential concern:** Reuse-by-default may surprise users who expect fresh
containers. v3 users migrating to v4 get different behavior unless they
explicitly use `WithoutReuse()`. The reuse key being `repository:tag` means
containers with different env vars but same image:tag will incorrectly reuse.

## 6. Resource Lifecycle & Cleanup

### v4

- `Resource.Close(ctx)` — stops then removes container with volumes
  (`resource.go:79-101`). Tolerates already-removed containers.
- `Resource.Cleanup(t)` — registers `t.Cleanup` for automatic removal
  (`resource.go:114-121`).
- `Resource.CloseT(t)` — close with `t.Fatalf` on error (`resource.go:105-110`).
- `Pool.Close(ctx)` — cleans up ALL tracked containers and networks, then closes
  client (`pool.go:178-188`).
- `Pool.cleanup(ctx)` — uses `context.WithoutCancel` with 60s timeout
  (`pool.go:193-220`). Removes containers first, then networks.
- Resource/network tracking via `sync.Map` (`pool.go:102-152`).

### v3

- `Resource.Close()` — calls `pool.Purge(r)` (`dockertest.go:229`).
- `Pool.Purge(r)` — force-removes container with volumes (`dockertest.go:474`).
- `Resource.Expire(seconds)` — stops container after delay in a goroutine
  (`dockertest.go:233-240`). Fire-and-forget with no error handling.
- No `Pool.Close()`. No automatic cleanup of tracked resources.
- No `t.Cleanup` integration.

### Assessment

v4's lifecycle management is dramatically better:

- `Pool.Close` ensures all resources are cleaned up (no leaked containers).
- `Resource.Cleanup(t)` integrates with Go's test lifecycle.
- `context.WithoutCancel` in cleanup ensures resources are removed even on
  cancellation.
- Resource tracking via `sync.Map` provides a safety net.

v3's `Expire` method is a fire-and-forget goroutine with swallowed errors — an
anti-pattern. v4 removes `Expire` entirely, which is correct since
`Resource.Close` and `Pool.Close` handle cleanup properly.

## 7. Network Management

### v4

- `Pool.CreateNetwork(ctx, name, *NetworkCreateOptions)` (`network.go:51`).
- `NetworkCreateOptions` struct with `Driver`, `Labels`, `Options`, `Internal`,
  `Attachable`, `Ingress`, `EnableIPv6` (`network.go:39-47`).
- `Network.Close(ctx)` removes network (`network.go:158-170`).
- Automatic retry with custom subnet on "all predefined address pools have been
  fully subnetted" error (`network.go:95-141`).
- `Resource.ConnectToNetwork(ctx, net)` with automatic container re-inspect
  (`network.go:183-205`).
- `Resource.DisconnectFromNetwork(ctx, net)` (`network.go:210-233`).
- `Resource.GetIPInNetwork(net)` (`network.go:237-253`).
- Pool tracks networks and cleans them up on `Pool.Close`.

### v3

- `Pool.CreateNetwork(name, opts ...func(*dc.CreateNetworkOptions))`
  (`dockertest.go:507`).
- Network connection at run-time via `RunOptions.Networks` and
  `RunOptions.NetworkID` fields.
- `Pool.RemoveNetwork(network)` — disconnects all containers first, then removes
  (`dockertest.go:527-535`).
- `Pool.NetworksByName(name)` — list networks by name filter
  (`dockertest.go:513-526`).
- `Resource.ConnectToNetwork(network)` / `DisconnectFromNetwork` — also
  refreshes network info (`dockertest.go:178-224`).

### Assessment

v4's network management is more robust:

- The subnet retry logic (`retryNetworkCreateWithCustomSubnet`) handles a common
  CI/CD issue where Docker runs out of predefined subnets.
- Pool-level network tracking ensures cleanup.
- Context support for all network operations.

**What v4 drops:**

- `Pool.NetworksByName` — no equivalent for listing/querying networks.
- `Pool.RemoveNetwork` with automatic container disconnect — v4's
  `Network.Close` does not disconnect containers first; callers must do it
  manually. v3's approach was more forgiving.
- Connecting at run-time via options — v3 allowed `Networks` in `RunOptions` to
  connect at creation time. v4 requires a separate `ConnectToNetwork` call after
  `Run`. This is slightly less convenient but clearer.

## 8. Build & Image Management

### v4

- `Pool.BuildAndRun(ctx, name, *BuildOptions, ...RunOption)` (`build.go:77`).
- `BuildOptions` with `Dockerfile`, `ContextDir`, `Tags`, `BuildArgs`
  (`map[string]*string`), `Labels`, `NoCache`, `ForceRemove` (`build.go:27-57`).
- Custom `createBuildContext` streams tar archive via pipe (`build.go:190-285`).
- `drainBuildStream` parses JSON build output for errors (`build.go:291-303`).
- Uses `distribution/reference` for image name parsing (`build.go:174-187`).
- Auto-adds `noPull` flag for locally built images (`build.go:142-148`).

### v3

- `Pool.BuildAndRun(name, dockerfilePath, env)` (`dockertest.go:460`).
- `Pool.BuildAndRunWithOptions(dockerfilePath, *RunOptions, ...func(*dc.HostConfig))`
  (`dockertest.go:448`).
- `Pool.BuildAndRunWithBuildOptions(*BuildOptions, *RunOptions, ...func(*dc.HostConfig))`
  (`dockertest.go:433`).
- `BuildOptions` with `Dockerfile`, `ContextDir`, `BuildArgs` (`[]dc.BuildArg`),
  `Platform`, `Version` (BuildKit selector), `Auth` (`dockertest.go:424-432`).
- Build delegates to `dc.Client.BuildImage` directly.

### Assessment

v4's build implementation is more thorough:

- Error handling via `drainBuildStream` catches build failures embedded in the
  JSON stream (Docker returns errors as stream messages, not HTTP errors).
- Image cleanup on build failure prevents dangling images.
- Streaming tar creation via pipe is memory-efficient.

**What v4 drops:**

- `Platform` field — no BuildKit platform support.
- `Version` field — no BuildKit v2 builder selection.
- `Auth` / `AuthConfigurations` — no registry authentication for builds.
- Multiple build convenience methods — v3 had three `BuildAndRun*` variants.

## 9. Retry Logic

### v4

- `Retry(ctx, timeout, interval, fn)` — constant backoff (`retry.go:15-30`).
- `RetryWithBackoff(ctx, timeout, initial, max, fn)` — exponential backoff with
  zero randomization (`retry.go:37-57`).
- `Pool.Retry(ctx, timeout, fn)` — convenience with 1s interval, falls back to
  `Pool.MaxWait` (`retry.go:61-66`).
- Uses `cenkalti/backoff/v5` with context support.
- Deterministic: `RandomizationFactor = 0`.

### v3

- `Pool.Retry(op)` — exponential backoff, 5s max interval, `Pool.MaxWait`
  timeout (`dockertest.go:480-495`).
- Uses `cenkalti/backoff/v4` (no context support in v4 of that library).
- Non-deterministic: uses default randomization factor.

### Assessment

v4's retry is better in every way:

- Context-aware (can be cancelled).
- Deterministic (no random jitter) — important for reproducible tests.
- More flexible with separate `Retry` and `RetryWithBackoff` functions.
- `Pool.Retry` is simpler — just pass timeout and function.

## 10. Error Handling

### v4

- Sentinel errors in `errors.go`: `ErrImagePullFailed`,
  `ErrContainerCreateFailed`, `ErrContainerStartFailed`, `ErrClientClosed`.
- Error wrapping with `fmt.Errorf("... %w", sentinel, err)` for chained
  `errors.Is` checks.
- `errdefs.IsNotFound(err)` from `containerd/errdefs` for Docker-specific error
  classification (`resource.go:92`, `pool.go:310`).

### v3

- Single sentinel: `ErrNotInContainer` (`dockertest.go:22`).
- Error wrapping with `fmt.Errorf("... %w", err)`.
- No structured error classification for Docker errors.
- `Retry` wraps timeout as `"reached retry deadline: %w"`.

### Assessment

v4's error handling is more structured. Sentinel errors allow callers to
programmatically distinguish failure modes. The use of `errdefs.IsNotFound` is
correct for Docker "not found" error classification instead of string matching.

## 11. Test Helpers & Testing Patterns

### v4

- `TestingTB` interface (`pool.go:22-28`) with `Helper()`, `Context()`,
  `Cleanup()`, `Logf()`, `Fatalf()`.
- `*T` variants for all operations: `NewPoolT`, `RunT`, `CloseT`,
  `CreateNetworkT`, `BuildAndRunT`.
- `Resource.Cleanup(t)` registers `t.Cleanup`.
- Tests use stdlib `testing` exclusively — no `testify`.
- Tests use `t.Context()` for context, `t.Cleanup` for cleanup.
- Unit tests for port/IP helpers use constructed types without Docker
  (`resource_test.go`).

### v3

- No test helper variants.
- Tests use `testify/require` and `testify/assert`.
- Global `pool` variable in `TestMain` — shared mutable state.
- Tests use `defer resource.Close()` — prone to leaking on `t.Fatal`.

### Assessment

v4's `*T` helpers and `Cleanup(t)` are significant ergonomic improvements. They
eliminate boilerplate error checking in tests and integrate with Go's test
lifecycle. The `TestingTB` interface enables use with `testing.T` and custom
test wrappers.

v4's choice to drop `testify` in favor of stdlib `testing` is opinionated but
reduces dependencies and makes the test code more portable.

## 12. Exec API

### v4

```go
result, err := resource.Exec(ctx, []string{"echo", "hello"})
// result.StdOut, result.StdErr, result.ExitCode
```

- `ExecResult` struct with `StdOut`, `StdErr`, `ExitCode`
  (`resource.go:186-190`).
- Uses `stdcopy.StdCopy` for stream demultiplexing.
- Context-aware.

### v3

```go
exitCode, err := resource.Exec(cmd, ExecOptions{StdOut: &buf, Env: env, TTY: true})
```

- Returns `(exitCode int, err error)`.
- `ExecOptions` struct with `Env`, `StdIn`, `StdOut`, `StdErr`, `TTY`.
- Caller provides io.Writer targets.

### Assessment

v4's Exec is simpler for common cases — just pass command, get result struct.
But v4 loses:

- `Env` — cannot set exec-specific environment variables.
- `StdIn` — cannot pipe input to the exec.
- `TTY` — cannot allocate TTY.
- Streaming — v4 buffers everything into strings; v3 allows streaming to any
  `io.Writer`.

For simple "run command, get output" cases, v4 is better. For advanced cases
(interactive commands, streaming large output, custom env), v3 was more capable.

## 13. Features Present in v3 but Missing in v4

| Feature                     | v3                                     | v4                                             |
| --------------------------- | -------------------------------------- | ---------------------------------------------- |
| Container names             | `RunOptions.Name`                      | Must use `WithContainerConfig`                 |
| Network join at run-time    | `RunOptions.Networks`                  | Separate `ConnectToNetwork` call               |
| Platform selection          | `RunOptions.Platform`                  | Not available                                  |
| Registry authentication     | `RunOptions.Auth`, `BuildOptions.Auth` | Not available                                  |
| TTY support                 | `RunOptions.Tty`, `ExecOptions.TTY`    | Not directly available                         |
| Container expiry            | `Resource.Expire(seconds)`             | Removed (use `Close`)                          |
| Container by name lookup    | `Pool.ContainerByName(name)`           | Not available                                  |
| Remove container by name    | `Pool.RemoveContainerByName(name)`     | Not available                                  |
| Current container detection | `Pool.CurrentContainer()`              | Not available                                  |
| Network listing             | `Pool.NetworksByName(name)`            | Not available                                  |
| Network auto-disconnect     | `Pool.RemoveNetwork` disconnects first | Manual disconnect required                     |
| TLS pool                    | `NewTLSPool(endpoint, certpath)`       | Use `DOCKER_CERT_PATH` env or `WithMobyClient` |
| Public client access        | `pool.Client` (public field)           | Private; inject via `WithMobyClient`           |
| Exec streaming              | `ExecOptions.StdOut/StdErr` io.Writer  | Buffered string result                         |
| Exec environment            | `ExecOptions.Env`                      | Not available                                  |
| BuildKit support            | `BuildOptions.Version`                 | Not available                                  |

## 14. Features New in v4 (Not in v3)

| Feature               | Description                                                    |
| --------------------- | -------------------------------------------------------------- |
| Context support       | All operations accept `context.Context`                        |
| Container reuse       | Automatic reuse via global registry                            |
| Pool.Close            | Automatic cleanup of all tracked resources                     |
| Resource.Cleanup(t)   | `t.Cleanup` integration                                        |
| \*T helper methods    | `NewPoolT`, `RunT`, `CloseT`, `CreateNetworkT`, `BuildAndRunT` |
| Functional options    | `WithTag`, `WithEnv`, `WithCmd`, etc.                          |
| Scoped registry       | Isolated reuse scopes per client                               |
| Client interface      | `DockerClient` interface for testability                       |
| Subnet retry          | Auto-retry network creation with custom subnets                |
| Resource.Logs         | Get container logs with stream demux                           |
| Deterministic retry   | Zero-jitter backoff                                            |
| Sentinel errors       | `ErrImagePullFailed`, `ErrContainerCreateFailed`, etc.         |
| Build error detection | JSON stream parsing for build errors                           |

## 15. Summary

### v4 Strengths

1. **Modern Go patterns** — context propagation, functional options, error
   wrapping with sentinels, `t.Cleanup` integration.
2. **Container reuse** — the marquee feature, providing 2-3x faster test suites.
3. **Lifecycle management** — `Pool.Close` cleans up everything; no leaked
   containers.
4. **Minimal dependencies** — 5 direct deps vs 20+ in v3; no vendored Docker
   client fork.
5. **Testable architecture** — `DockerClient` interface enables unit testing.
6. **Clean code organization** — 12 focused files vs monolithic single file.

### v4 Regressions / Concerns

1. **Dropped features** — no Platform, Auth, TTY, Exec env/stdin, container
   names in options, BuildKit support.
2. **Reuse-by-default gotcha** — containers with same image:tag but different
   env/config will incorrectly reuse. Users must know about `WithoutReuse` or
   `WithReuseID`.
3. **Less discoverable advanced options** —
   `WithContainerConfig`/`WithHostConfig` modifiers are escape hatches but not
   self-documenting like v3's struct fields.
4. **No streaming exec** — v4 buffers all exec output into strings; unsuitable
   for large output or interactive commands.
5. **No public client access** — users who need raw Docker operations must
   inject their own client.
6. **Network auto-disconnect removed** — v3's `RemoveNetwork` auto-disconnected
   containers; v4 requires manual disconnect before network removal.

### Design Philosophy

v4 adopts a "make the common case easy, provide escape hatches for the rest"
philosophy. For 90% of use cases (start container, wait for readiness, run
tests, clean up), v4 is dramatically simpler and safer than v3. The 10% of
advanced use cases (Platform, Auth, TTY, streaming) require workarounds or are
not yet supported.
