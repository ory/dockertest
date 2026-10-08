---
date: 2026-10-08
reason:
  Implementation notes for the v4 resource ownership, bounded cleanup, build
  image lifetime, Main adapter, and abandoned-run recovery work, so that the
  design decisions behind owner.go, main.go, and recovery.go are recorded.
---

# Resource ownership and cleanup

## Ownership model

- `processOwner` (owner.go) is the single process-wide inventory of physical
  containers, networks, and built images. It is separate from the reuse registry
  (registry.go), which only tracks handles to reusable containers.
- `resource` and `dockerNetwork` are handles. Each handle releases exactly once
  (`atomic.Bool`), so repeated or concurrent `Close` calls are safe. The last
  handle of a reused container triggers the physical removal.
- Removals of the same resource are shared: a handle's `Close`, the pool's
  `Close`, and `Main` wait on the same in-flight attempt instead of issuing a
  second request. A failed removal keeps the record for retry.
- Containers are tracked immediately after `ContainerCreate`, before start and
  inspection, so every rollback path goes through the same bounded removal
  (`Force`, `RemoveVolumes`). Rollback errors are joined with the operation
  error, so `errors.Is` still works for both.

## Cleanup budgets

- Every cleanup derives its context with
  `context.WithTimeout(ctx, cleanupTimeout)`. A nested cleanup can only shorten
  the outer deadline, never reset or extend it. Rollbacks use
  `context.WithoutCancel` first so that a canceled operation context does not
  abort the rollback.
- Without `Main`, a pool closes its own client only after a successful cleanup.
  Under `Main`, pools keep their clients open and `Main` closes them last, so
  the final retry and the label-based reconciliation have working clients.

## Build images

- The daemon reports the result as `{"aux":{"ID":...}}` (classic builder) or
  with `"id":"moby.image.id"` (BuildKit). The ID is accepted only after the
  stream ends cleanly; tags are never inspected to find the result.
- Containers are created from the image ID and reuse keys include it, so a
  changed build under one tag gets a fresh container.
- Ownership labels live under `io.ory.dockertest.*`. Default builds carry the
  run ID, so a different run produces a different image ID even for an unchanged
  context. `RetainImage` uses stable labels without a run ID.
- Image users = pending builds + live containers whose inspected image ID is an
  owned image. Removal is by full ID, never forced; conflicts are warnings.
- Failed builds never remove tags. Creation responses that were lost are
  reconciled at `Main` shutdown by listing resources with this run's labels.

## Main and recovery

- `Main` installs into the owner (rejects repeated installs and resources
  created earlier), takes the run lock, writes the manifest, and wires a
  per-daemon hook that runs recovery before the first creation on that daemon.
- Shutdown is one path for normal completion and signals: stop admitting, cancel
  in-flight operations, drain, package `Cleanup`, Docker cleanup,
  reconciliation, close owned clients, release the run record. A timer enforces
  the budget even when a callback ignores its context.
- Locks: `flock(LOCK_EX|LOCK_NB)` on Unix, `LockFileEx` with
  `LOCKFILE_FAIL_IMMEDIATELY` on Windows. Lock files are never unlinked to avoid
  unlink/recreate races. Manifests are removed only after a clean shutdown or a
  successful recovery of every listed daemon.
- Recovery verifies the full tuple (managed, scope, host, run) on every resource
  before deletion and never uses age, PID, or names.

## Deferred

- Cross-host recovery, cache eviction of retained images, Compose stacks, named
  volumes, and unlabeled resources stay out of automatic cleanup.
- The Windows signal and lock tests are written against documented Go and Win32
  behavior but were only compiled, not executed, during development on macOS; CI
  runs them on windows-latest.
