---
date: 2026-02-24
reason: neutral synthesis of bidirectional v3/v4 comparison reports
---

# Neutral Synthesis: dockertest v3 vs v4

This report synthesizes the v3-baseline report and v4-baseline report, adds
independent source code analysis, and provides actionable recommendations.

## 1. Agreements (Both Reports Align)

Both reports agree on the following assessments:

1. **Context propagation is a major improvement.** v4's universal
   `context.Context` support enables cancellation, timeout propagation, and
   standard Go concurrency patterns. v3 has zero context support.

2. **Dependency reduction is significant.** v4 drops the vendored
   `docker/` subpackage (20+ files) and reduces direct dependencies from 20+
   to 5. The switch from `fsouza/go-dockerclient` fork to `moby/moby/client`
   is correct.

3. **Code organization is better in v4.** The monolithic `dockertest.go`
   (707 lines) is split into focused files (`pool.go`, `resource.go`,
   `network.go`, `build.go`, `options.go`, `registry.go`, `retry.go`,
   `errors.go`).

4. **Container reuse is v4's flagship feature.** Both reports identify it as
   valuable for test performance, but both flag the reuse-by-default behavior
   as potentially surprising.

5. **Lifecycle management is dramatically better.** `Pool.Close` with resource
   tracking, `t.Cleanup` integration, and `context.WithoutCancel` in cleanup
   paths are clear improvements.

6. **v4 drops many v3 features.** Both reports enumerate the same set of
   removed capabilities (Platform, Auth, TTY, Exec env/stdin, container names
   in options, BuildKit, `ContainerByName`, `CurrentContainer`,
   `NetworksByName`, `Expire`).

7. **Functional options are more idiomatic** than v3's 20+ field struct, but
   less discoverable for advanced use cases.

8. **Retry is better in v4.** Context-aware, deterministic (no jitter),
   more flexible with separate `Retry` and `RetryWithBackoff`.

9. **Error handling is more structured.** Sentinel errors
   (`ErrImagePullFailed`, etc.) and `errdefs.IsNotFound` are improvements
   over v3's ad-hoc error handling.

## 2. Gaps (What One Report Caught That the Other Missed)

### Caught by v3-baseline report only

- **`inspectContainerWithRetries` removal**: v3 retried container inspection
  10 times with 100ms sleep to handle race conditions where Docker reports
  empty port bindings immediately after container start. v4 removes this
  entirely (`pool.go:382`). This could cause flaky port lookups on slow
  Docker hosts or under load.

- **`RemoveNetwork` auto-disconnect**: v3's `RemoveNetwork` iterates all
  connected containers and disconnects them before removing the network
  (`dockertest.go:698-707`). v4's `Network.Close` (`network.go:158-170`)
  just calls `NetworkRemove` and will fail if containers are still connected.
  The doc comment warns about this but it's a behavioral regression.

- **`Container` field type change**: v3 uses `*dc.Container` (pointer), v4
  uses `container.InspectResponse` (value). This is a subtle breaking change
  for anyone checking `resource.Container == nil`.

### Caught by v4-baseline report only

- **Reuse key collision**: Containers with the same `repository:tag` but
  different env vars, commands, or host config will incorrectly reuse.
  `computeReuseID` (`pool.go:276-284`) only considers repository and tag,
  not configuration. This is a correctness hazard.

- **Streaming exec loss**: v3 allowed `io.Writer` targets for exec output
  (streaming large output). v4 buffers everything into strings
  (`resource.go:213-214`). For containers producing large exec output, this
  will cause memory issues.

### Caught by neither report

- **`NewPoolT` signature inconsistency**: `NewPoolT` takes `*testing.T`
  (`pool.go:155`) while all other `*T` methods take `TestingTB`. This means
  `NewPoolT` cannot be used with custom test wrappers that implement
  `TestingTB`. Every other T-helper (`RunT`, `CloseT`, `CreateNetworkT`,
  `BuildAndRunT`) accepts `TestingTB`.

- **`Logs` re-implements Docker stream demux**: `resource.go:140-180`
  manually parses the Docker multiplexed stream format (8-byte header +
  payload), while `Exec` at `resource.go:214` uses `stdcopy.StdCopy` from
  `github.com/moby/moby/api/pkg/stdcopy` which does exactly the same thing.
  The `Logs` implementation is redundant code that could use `stdcopy.StdCopy`.

- **`Network.Close` does not tolerate already-removed networks**:
  `Resource.Close` (`resource.go:92`) checks `errdefs.IsNotFound` and
  tolerates already-removed containers. `Network.Close` (`network.go:163`)
  does not — it returns the raw error if the network was already removed.
  This is an inconsistency in cleanup behavior.

- **`Pool.Close` calls cleanup then closes client, but cleanup errors are
  swallowed when client close also errors**: `pool.go:178-188` returns
  `cleanupErr` only if client close succeeds. If both fail, the cleanup
  error is lost.

- **No `WithName` option**: v4 has no dedicated `WithName(name string)`
  option for container naming. Users must use `WithContainerConfig` to set
  `Config.Hostname` and the container name separately. Container naming is
  a common enough operation to warrant a first-class option.

- **`PoolOption` returns nothing** (`options.go:15`): `PoolOption` is
  `func(*Pool)` with no error return, while `RunOption` is
  `func(*runConfig) error`. This is an inconsistency — if a future pool
  option needs validation, the signature must change (breaking).

## 3. Broken/Inconsistent Behavior

### 3.1 Reuse-by-default is a correctness hazard

The reuse key `repository:tag` (`pool.go:283`) means:

```go
pool.Run(ctx, "postgres", WithTag("14"), WithEnv([]string{"POSTGRES_PASSWORD=a"}))
pool.Run(ctx, "postgres", WithTag("14"), WithEnv([]string{"POSTGRES_PASSWORD=b"}))
```

The second call silently reuses the first container with password `a`. This
is correct for the "same image, same config" case but dangerous when configs
differ. The v3 behavior (always create new) was safer by default.

**Recommendation**: Either hash the full config into the reuse key, or
document this prominently and require explicit opt-in to reuse.

### 3.2 Network cleanup asymmetry

`Resource.Close` tolerates already-removed containers
(`resource.go:92`: `errdefs.IsNotFound`). `Network.Close` does not
(`network.go:163`). During `Pool.cleanup`, if a network was removed
externally, the cleanup will record an error.

**Recommendation**: Add `errdefs.IsNotFound` tolerance to `Network.Close`.

### 3.3 Missing port binding retry

v3's `inspectContainerWithRetries` handled a real Docker race condition
where port bindings are not immediately available after `ContainerStart`.
v4 removes this entirely. Users relying on `GetPort` immediately after
`Run` may get empty strings on slower Docker hosts.

**Recommendation**: Either re-add a short inspection retry in
`inspectAndRegister` or document that `Retry` should wrap port lookups.

### 3.4 `Logs` manual demux vs `stdcopy`

`resource.go:140-180` manually parses Docker's multiplexed log stream
(8-byte header with stream type, padding, and big-endian size, followed by
payload). This is the exact format that `stdcopy.StdCopy` handles, and
`stdcopy` is already imported and used by `Exec` (`resource.go:214`).

The manual implementation adds size-limit checks (64 MiB per message,
256 MiB total) which `stdcopy` does not, but these could be implemented
with a `LimitedReader` wrapper instead.

**Recommendation**: Replace manual demux with `stdcopy.StdCopy` to a
`bytes.Buffer`, optionally wrapped with size limits.

## 4. Things Done Well

### v4 excels at

1. **Test ergonomics**: The `*T` helpers (`NewPoolT`, `RunT`, `CloseT`,
   `BuildAndRunT`, `CreateNetworkT`) and `Resource.Cleanup(t)` eliminate
   boilerplate. This is the biggest day-to-day improvement for users.

2. **Cleanup safety**: `Pool.Close` with `context.WithoutCancel` and
   60-second timeout ensures resources are cleaned up even when tests are
   cancelled. v3 leaked containers on panic or `t.Fatal`.

3. **Network subnet retry** (`network.go:95-141`): Automatically retries
   network creation with custom subnets when Docker's pool is exhausted.
   This handles a real CI/CD pain point that v3 users had to work around
   manually.

4. **Client abstraction** (`internal/client/interface.go`): The
   `DockerClient` interface enables unit testing without Docker and supports
   client injection via `WithMobyClient`. v3's exported concrete client
   was convenient but untestable.

5. **Build error detection** (`build.go:291-303`): `drainBuildStream`
   catches errors embedded in Docker's JSON build output stream. v3
   delegated to the vendored client which could miss these.

6. **Race-safe reuse** (`pool.go:394-408`): The `LoadOrStore` +
   cleanup-duplicate pattern correctly handles concurrent registration.

### v3 excels at

1. **Feature completeness**: Platform selection, registry auth, TTY, exec
   env/stdin/streaming, container naming, `ContainerByName`, `CurrentContainer`,
   `NetworksByName`, `BuildKit` support, `Expire`. These cover real use cases.

2. **Discoverability**: `RunOptions` struct with 20+ named fields is
   self-documenting. You can see all options in one place. v4's escape
   hatches (`WithContainerConfig`, `WithHostConfig`) require knowing the
   moby API types.

3. **Network removal resilience**: `RemoveNetwork` auto-disconnects
   containers, which is more forgiving in cleanup scenarios.

4. **`inspectContainerWithRetries`**: Handles a real Docker race condition.

## 5. Opportunities

### 5.1 High-priority additions

| Feature | Effort | Impact | Rationale |
|---------|--------|--------|-----------|
| `WithName(name)` option | Low | Medium | Container naming is common; escape hatch is clumsy |
| `WithPortBindings(...)` option | Low | High | Port binding is the #1 use case after env vars |
| `WithExposedPorts(...)` option | Low | Medium | Explicit port exposure is common |
| `WithMounts(...)` option | Low | High | Volume mounts are extremely common |
| `WithNetwork(net)` option | Low | Medium | Connect at creation time, not after |
| Fix `NewPoolT` to accept `TestingTB` | Trivial | Low | Consistency with all other T-helpers |
| Fix `Network.Close` not-found tolerance | Trivial | Low | Consistency with `Resource.Close` |

### 5.2 Medium-priority additions

| Feature | Effort | Impact | Rationale |
|---------|--------|--------|-----------|
| Config-aware reuse keys | Medium | High | Prevent silent reuse with different env/cmd |
| `WithPlatform(platform)` option | Low | Medium | Multi-arch testing is increasingly common |
| Port binding retry in `inspectAndRegister` | Low | Medium | Prevent flaky port lookups |
| Replace `Logs` manual demux with `stdcopy` | Low | Low | Remove duplicated code |
| `PoolOption` error return | Medium | Low | Future-proofing, but breaking |

### 5.3 Low-priority / nice-to-have

| Feature | Effort | Impact | Rationale |
|---------|--------|--------|-----------|
| Registry auth for pulls/builds | Medium | Low | Only needed for private registries |
| Exec env/stdin support | Medium | Low | Niche use case |
| `Pool.ContainerByName` | Low | Low | Useful for debugging, not critical |
| Streaming exec output | Medium | Low | Only needed for large output |

## 6. Migration Risk Assessment

### Breaking changes requiring code modification

Every v3 call site needs changes:

1. **All functions gain `context.Context`** - mechanical but pervasive.
2. **`Pool.Run(repo, tag, env)` becomes `Pool.Run(ctx, repo, opts...)`** -
   tag and env move to options.
3. **`RunOptions` struct users** must switch to functional options or escape
   hatches.
4. **`pool.Client` access** is gone. Users calling Docker API directly need
   `WithMobyClient` to inject and retain a reference.
5. **`pool.Purge(r)` becomes `r.Close(ctx)`**.
6. **`Resource.Container` type changes** from `*dc.Container` to
   `container.InspectResponse`. All field access paths change
   (e.g., `r.Container.NetworkSettings.Ports[dc.Port("5432/tcp")]` becomes
   `r.Container.NetworkSettings.Ports[port]` with `network.ParsePort`).
7. **Error types change** - `dc.NoSuchContainer` becomes
   `errdefs.IsNotFound`.

### Behavioral changes (no compiler error, but different runtime behavior)

1. **Container reuse**: v3 always creates new containers. v4 reuses by
   default. Tests assuming fresh containers will silently get stale ones.
2. **Retry backoff**: v3 uses exponential with jitter. v4 uses constant
   1-second interval. Test timing may change.
3. **`Resource.Close`**: v3 only force-removes. v4 stops then removes.
   This is safer but slightly slower.

### Migration difficulty: **High**

- Every call site changes (context parameter).
- Type system changes (`dc.*` types to `moby/*` types) require updating
  all field access.
- Behavioral changes (reuse) can cause subtle test failures.
- Missing features (auth, platform, exec streaming) may block some users.

A migration guide with before/after examples for each pattern would
significantly reduce adoption friction.

## 7. Actionable Recommendations (Prioritized)

### Must-fix before release

1. **Fix `NewPoolT` to accept `TestingTB`** instead of `*testing.T`
   (`pool.go:155`). This is a trivial change that fixes an API
   inconsistency. Every other T-helper uses `TestingTB`.

2. **Add `errdefs.IsNotFound` tolerance to `Network.Close`**
   (`network.go:163`). Match `Resource.Close` behavior for consistent
   cleanup.

3. **Document reuse-by-default behavior prominently** in `doc.go` and
   `Run` godoc. Warn that containers with same image:tag but different
   config will reuse. Show `WithoutReuse()` and `WithReuseID()` examples.

### Should-fix before release

4. **Add `WithName`, `WithPortBindings`, `WithMounts`** options. These
   are the most commonly used v3 `RunOptions` fields and their absence
   forces users into escape hatches for basic operations.

5. **Replace `Logs` manual demux** (`resource.go:140-180`) with
   `stdcopy.StdCopy`. The current code duplicates functionality already
   available via an imported package.

6. **Consider port binding retry** in `inspectAndRegister` or document
   that `Retry` should be used around `GetPort` calls.

### Nice-to-have before release

7. **Add `WithNetwork` option** to connect at creation time.

8. **Write a migration guide** with v3-to-v4 before/after examples for
   common patterns (basic run, run with options, build, network, exec,
   retry, cleanup).

9. **Consider config-aware reuse keys** — hash env, cmd, entrypoint,
   labels into the reuse ID to prevent silent misconfiguration.

10. **Add `WithPlatform` option** for multi-arch testing support.
