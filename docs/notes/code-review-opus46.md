---
date: 2026-02-06
reason:
  Critical code review of entire Go codebase by Opus 4.6, finding issues from
  earlier AI-generated code
---

# Code Review: ory/dockertest v4

Reviewed all Go source files (~6,800 lines, 24 files) with 6 parallel review
agents.

## BUGS

### B1: `context.Background()` in `Resource.Cleanup` (resource.go:115)

```go
_ = r.Close(context.Background())
```

Violates project rule: "Always pass context.Context from caller (never use
context.Background or TODO)." Should use `context.WithoutCancel(t.Context())`,
exactly as `CloseT` does on line 105. The `t TestingTB` parameter provides
`Context()`.

### B2: `err == io.EOF` instead of `errors.Is` (resource.go:142)

```go
if err == io.EOF {
```

Violates project rule: "Do not use equality for error comparison. Always use
errors.Is()." Line 145 in the same function correctly uses
`errors.Is(err, io.ErrUnexpectedEOF)`.

### B3: `Remove` field logic is inverted (build.go:113)

```go
Remove: buildOpts.Remove || !buildOpts.ForceRemove, // default to true
```

When user sets `ForceRemove=true` (leaving `Remove` at zero value `false`):
`false || !true = false`. Setting ForceRemove actually **disables** Remove. Fix
with `!buildOpts.ForceRemove` -> use a `NoRemove` bool, or hardcode `true`.

### B4: Build errors in JSON stream silently discarded (build.go:127-133)

```go
if _, drainErr := io.Copy(io.Discard, buildResult.Body); drainErr != nil {
```

Docker build API returns errors _inside_ the JSON stream body as
`{"error":"..."}` messages, not as HTTP errors. Draining to `io.Discard` means a
failing `RUN` command, Dockerfile syntax error, etc. is silently swallowed.
`BuildAndRun` appears to succeed, then `Run` fails with confusing "image not
found".

### B5: Symlinks broken in tar archive (build.go:222)

```go
header, err := tar.FileInfoHeader(info, "")
```

The second argument to `FileInfoHeader` is the symlink target. Passing `""`
means symlinks get empty targets. The walk callback then opens the file
(`os.Open(path)` follows the symlink) and writes content, but the tar header
says `TypeSymlink` with `Linkname: ""`. This is an inconsistent tar entry.

### B6: `append` may mutate caller's slice (build.go:146)

```go
allOpts := append(runOpts, noPullOpt, WithTag(tag))
```

If `runOpts` has spare capacity, this mutates the caller's underlying array.
Well-known Go gotcha. Use `slices.Concat` or explicitly allocate.

### B7: Non-deterministic subnet seeding (network.go:102)

```go
seed := time.Now().UnixNano()
```

Uses time-based seeding in production code, making network creation behavior
vary between runs. A deterministic approach (e.g., hash of network name) would
be equally effective at avoiding collisions and easier to debug.

## ISSUES

### I1: `Pool.Close()` not truly idempotent (pool.go:161-166)

Doc says "safe to call Close multiple times" but after first call, `p.client` is
still non-nil and `p.ownedClient` is still true. Second call calls
`p.client.Close()` again. Should set `p.client = nil` after first close.

### I2: Shallow clone of Resource shares InspectResponse (pool.go:260-261)

```go
cloned := *existing
cloned.pool = p
return &cloned
```

`container.InspectResponse` contains maps/slices. The clone and original share
underlying data. `ConnectToNetwork` reassigns `r.Container` (network.go:191)
which is safe per-field, but concurrent access to the shared registry entry
could race.

### I3: Error string matching in network retry (network.go:98, 125-126)

```go
strings.Contains(createErr.Error(), "all predefined address pools have been fully subnetted")
```

Fragile. Docker daemon message changes silently break this. Should use typed
error checks where possible.

### I4: `Register()` always returns nil (registry.go:49-52)

The `error` return is misleading. Either validate inputs or remove the error
return type.

### I5: No bounds check on log message size (resource.go:163)

```go
message := make([]byte, size)
```

`size` is a `uint32` from Docker stream. A malformed stream can set
`size=math.MaxUint32` causing 4GiB allocation / OOM.

### I6: Dead code in log demux (resource.go:148)

```go
if n == 0 { break }
```

After `io.ReadFull` returns `io.EOF` (handled line 142) and non-ErrUnexpectedEOF
errors (handled line 145), `n` can never be 0 at line 148.

### I7: `Cleanup` swallows close error silently (resource.go:115)

Compare with `NewPoolT` (pool.go:148-149) which reports errors via `t.Errorf`.
`Cleanup` should at minimum log cleanup failures.

### I8: `ConnectToNetwork`/`DisconnectFromNetwork` return nil on nil pool (network.go:172-174, 199-201)

Silently returns nil on what is a programming error. Should return an error.
`Resource.Logs` (resource.go:123) correctly returns an error in this case.

### I9: TOCTOU race in container reuse (pool.go:210-227)

Two goroutines calling `Run` concurrently with same `reuseID` both pass
`checkForExisting`, both create containers, then `inspectAndRegister` cleans up
the duplicate. This works but wastes Docker resources. A `singleflight` approach
keyed on `reuseID` would be the proper fix.

### I10: `Pool.MaxWait` is dead configuration (pool.go:40, retry.go:61)

`Pool.Retry` requires the caller to pass `timeout` explicitly and ignores
`MaxWait`. The field is set to 60s by default but never read.

### I11: `createBuildContext` doesn't validate contextDir exists (build.go:190)

If `contextDir` doesn't exist, the caller gets nil error from
`createBuildContext`, then `ImageBuild` gets a confusing pipe read error. Should
fast-fail by checking the directory exists before launching the goroutine.

### I12: No `.dockerignore` support (build.go:205-253)

Archives entire directory tree including `.git`, secrets, etc. Deviates from
standard Docker behavior.

### I13: `filepath.Walk` vs `filepath.WalkDir` (build.go:205)

`filepath.WalkDir` (Go 1.16+) avoids `os.Lstat` per file. Better performance for
large build contexts.

### I14: `clientScope` uses reflect for pointer-based scoping (pool.go:98-110)

Overly complex. Since `DockerClient` is always a pointer type,
`fmt.Sprintf("%p", c)` suffices.

## TEST GAPS

### T1: `time.Sleep(2*time.Second)` in build_test.go:185

Classic flakiness source. Should use `pool.Retry()` to poll for log output.

### T2: Non-deterministic `uniqueNetworkName` (network_test.go:15)

Uses `time.Now().UnixNano()`. Violates CLAUDE.md determinism rule. Use atomic
counter.

### T3: Tests verify non-nil but not actual behavior

`TestBuildAndRunWithBuildArgs` never verifies build args took effect.
`TestBuildAndRunWithRunOptions` sets `WithEnv([]string{"TEST_VAR=hello"})` but
never checks the env was applied. These pass even if the features are broken.

### T4: Missing test coverage

- `BuildAndRun` with nil/empty opts (validation paths)
- `Resource.Logs()` edge cases
- `Resource.Close()` error paths (nil pool, double-close)
- `Resource.Cleanup(t)` method
- `Pool.Run` sentinel error wrapping
- `retryNetworkCreateWithCustomSubnet` fallback
- `WithEntrypoint` option
- `splitImageReference` edge cases
- `NewPool` with non-empty endpoint

### T5: Weak IP validation (network_test.go:228-230)

```go
if len(ip) < 7 {
```

`"abcdefg"` passes. Use `netip.ParseAddr(ip)`.

### T6: `TestNetworkCloseT` has no assertions (network_test.go:128-137)

Comment says "Network should be removed" but no assertion verifies it.

## STRENGTHS

- Clean dependency compliance: no `github.com/docker/docker` imports
- Consistent `context.Context` propagation (except B1)
- Proper `context.WithoutCancel` for cleanup paths
- Thread-safe registry using `sync.Map` with `LoadOrStore`
- Good functional options pattern
- Clean `*T` test helper variants
- Deterministic retry (no jitter)
- Proper error wrapping with sentinel errors
- Tests use `t.Cleanup` consistently (never `defer`)
- Tests use `t.Context()` consistently
- Good `testing.Short()` gating for Docker-dependent tests
