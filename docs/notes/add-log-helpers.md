---
date: 2026-03-18
reason:
  Design notes for adding log helper options to v4, bridging the gap from v3's
  LogsOptions
---

# Add Log Helpers to v4

## Problem

v3 had rich `LogsOptions` (Tail, Since, Follow, separate stdout/stderr,
Timestamps) via `go-dockerclient`. v4 only has `Logs(ctx) (string, error)` which
returns combined stdout+stderr with no filtering options.

## Design

- Change `Logs()` return type from `string` to `LogResult{Stdout, Stderr}`
  (matches `ExecResult` pattern)
- Add `LogOption` functional options: `WithTail`, `WithLogsSince`,
  `WithLogsUntil`, `WithTimestamps`
- `LogResult.Combined()` provides backward-compat convenience
- Deliberately omit `Follow`/streaming — users can use
  `Pool.Client().ContainerLogs()` directly

## Files changed

- `resource.go` — interface + implementation
- `options.go` — LogOption, logConfig, LogResult types
- `build_test.go` — update 3 call sites
- `README.md`, `UPGRADE.md` — documentation
