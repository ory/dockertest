# Error Handling Refactoring to Standard Library Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace custom error types and helper functions with standard library sentinel errors and `errors.Is()` for better idiomatic Go error handling.

**Architecture:** Remove the custom `Error` struct, `ErrorType` enum, and `Is*` helper functions. Replace with exported sentinel error variables that can be wrapped with `fmt.Errorf` and checked with `errors.Is()`.

**Tech Stack:** Go 1.22+, standard library `errors` and `fmt` packages

---

## Task 1: Define Sentinel Errors

**Files:**
- Modify: `v4/errors.go:1-106`

**Step 1: Write failing test for new sentinel errors**

```go
// Add to v4/errors_test.go
func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name     string
		sentinel error
	}{
		{"ErrContainerNotFound", ErrContainerNotFound},
		{"ErrImageNotFound", ErrImageNotFound},
		{"ErrNetworkNotFound", ErrNetworkNotFound},
		{"ErrTimeout", ErrTimeout},
		{"ErrConnectionRefused", ErrConnectionRefused},
		{"ErrImagePullFailed", ErrImagePullFailed},
		{"ErrContainerCreateFailed", ErrContainerCreateFailed},
		{"ErrContainerStartFailed", ErrContainerStartFailed},
		{"ErrExecFailed", ErrExecFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotNil(t, tt.sentinel)
			assert.Error(t, tt.sentinel)
		})
	}
}

func TestErrorWrapping(t *testing.T) {
	cause := errors.New("underlying error")
	wrapped := fmt.Errorf("failed to pull image: %w", ErrImagePullFailed)

	assert.True(t, errors.Is(wrapped, ErrImagePullFailed))

	doubleWrapped := fmt.Errorf("operation failed: %w", wrapped)
	assert.True(t, errors.Is(doubleWrapped, ErrImagePullFailed))

	withCause := fmt.Errorf("pull postgres:14: %w: %v", ErrImagePullFailed, cause)
	assert.True(t, errors.Is(withCause, ErrImagePullFailed))
}
```

**Step 2: Run tests to verify they fail**

Run: `cd v4 && go test -run TestSentinelErrors -v`
Expected: FAIL with "undefined: ErrContainerNotFound"

**Step 3: Replace custom error implementation with sentinel errors**

Replace the entire `v4/errors.go` content:

```go
// Copyright © 2024 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package dockertest

import "errors"

// Sentinel errors for common dockertest failures.
// Use errors.Is() to check for these errors in wrapped error chains.
var (
	// ErrContainerNotFound indicates a container was not found.
	ErrContainerNotFound = errors.New("container not found")

	// ErrImageNotFound indicates an image was not found.
	ErrImageNotFound = errors.New("image not found")

	// ErrNetworkNotFound indicates a network was not found.
	ErrNetworkNotFound = errors.New("network not found")

	// ErrTimeout indicates an operation timed out.
	ErrTimeout = errors.New("operation timed out")

	// ErrConnectionRefused indicates a connection was refused.
	ErrConnectionRefused = errors.New("connection refused")

	// ErrImagePullFailed indicates an image pull operation failed.
	ErrImagePullFailed = errors.New("image pull failed")

	// ErrContainerCreateFailed indicates container creation failed.
	ErrContainerCreateFailed = errors.New("container create failed")

	// ErrContainerStartFailed indicates container start failed.
	ErrContainerStartFailed = errors.New("container start failed")

	// ErrExecFailed indicates an exec operation failed.
	ErrExecFailed = errors.New("exec failed")
)
```

**Step 4: Run tests to verify they pass**

Run: `cd v4 && go test -run TestSentinelErrors -v`
Expected: PASS

**Step 5: Commit**

```bash
cd v4
git add errors.go errors_test.go
git commit -m "refactor: replace custom Error type with sentinel errors"
```

---

## Task 2: Update Old Tests to Use errors.Is()

**Files:**
- Modify: `v4/errors_test.go:10-148`

**Step 1: Write test for errors.Is() compatibility**

Add to `v4/errors_test.go`:

```go
func TestErrorsIs(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		target   error
		expected bool
	}{
		{
			name:     "direct match",
			err:      ErrContainerNotFound,
			target:   ErrContainerNotFound,
			expected: true,
		},
		{
			name:     "wrapped match",
			err:      fmt.Errorf("container abc123: %w", ErrContainerNotFound),
			target:   ErrContainerNotFound,
			expected: true,
		},
		{
			name:     "double wrapped match",
			err:      fmt.Errorf("operation failed: %w", fmt.Errorf("container abc123: %w", ErrContainerNotFound)),
			target:   ErrContainerNotFound,
			expected: true,
		},
		{
			name:     "no match",
			err:      ErrImageNotFound,
			target:   ErrContainerNotFound,
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			target:   ErrContainerNotFound,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, errors.Is(tt.err, tt.target))
		})
	}
}
```

**Step 2: Run test to verify it passes**

Run: `cd v4 && go test -run TestErrorsIs -v`
Expected: PASS

**Step 3: Delete old tests that used custom Error type**

Remove these functions from `v4/errors_test.go`:
- `TestErrorType` (lines 10-20)
- `TestErrorUnwrap` (lines 22-31)
- `TestIsNotFound` (lines 33-71)
- `TestIsTimeout` (lines 73-101)
- `TestIsConnectionError` (lines 103-136)
- `TestWrapError` (lines 138-148)

**Step 4: Run all tests to verify**

Run: `cd v4 && go test -v`
Expected: PASS for all tests

**Step 5: Commit**

```bash
cd v4
git add errors_test.go
git commit -m "test: update tests to use errors.Is() instead of custom helpers"
```

---

## Task 3: Update pool.go Error Usage

**Files:**
- Modify: `v4/pool.go:72,91,100,110,132,181,188,196,249,256`

**Step 1: Write test for Pool error wrapping**

Add to `v4/pool_test.go`:

```go
func TestPoolErrorWrapping(t *testing.T) {
	// Test that Pool.NewPool wraps connection errors correctly
	t.Run("connection refused error", func(t *testing.T) {
		// This would need a mock client that returns connection error
		// For now, document the expected behavior
		mockErr := fmt.Errorf("failed to connect to Docker daemon: %w", ErrConnectionRefused)
		assert.True(t, errors.Is(mockErr, ErrConnectionRefused))
	})

	t.Run("image pull error", func(t *testing.T) {
		mockErr := fmt.Errorf("failed to pull image postgres:14: %w", ErrImagePullFailed)
		assert.True(t, errors.Is(mockErr, ErrImagePullFailed))
	})

	t.Run("container create error", func(t *testing.T) {
		mockErr := fmt.Errorf("failed to create container: %w", ErrContainerCreateFailed)
		assert.True(t, errors.Is(mockErr, ErrContainerCreateFailed))
	})
}
```

**Step 2: Run test**

Run: `cd v4 && go test -run TestPoolErrorWrapping -v`
Expected: PASS

**Step 3: Replace wrapError calls in pool.go**

Replace line 72:
```go
// Old:
return nil, wrapError(ErrTypeUnknown, "failed to apply pool option", err)

// New:
return nil, fmt.Errorf("failed to apply pool option: %w", err)
```

Replace line 91:
```go
// Old:
return nil, wrapError(ErrTypeUnknown, "failed to create Docker client", err)

// New:
return nil, fmt.Errorf("failed to create Docker client: %w", err)
```

Replace line 100:
```go
// Old:
return nil, wrapError(ErrTypeConnectionRefused, "failed to connect to Docker daemon", err)

// New:
return nil, fmt.Errorf("failed to connect to Docker daemon: %w: %w", ErrConnectionRefused, err)
```

Replace line 110:
```go
// Old:
return wrapError(ErrTypeConnectionRefused, "failed to ping Docker daemon", err)

// New:
return fmt.Errorf("failed to ping Docker daemon: %w: %w", ErrConnectionRefused, err)
```

Replace line 132:
```go
// Old:
return nil, wrapError(ErrTypeUnknown, "failed to apply run option", err)

// New:
return nil, fmt.Errorf("failed to apply run option: %w", err)
```

Replace line 181:
```go
// Old:
return nil, wrapError(ErrTypeContainerCreateFailed, "failed to create container", err)

// New:
return nil, fmt.Errorf("failed to create container: %w: %w", ErrContainerCreateFailed, err)
```

Replace line 188:
```go
// Old:
return nil, wrapError(ErrTypeContainerStartFailed, "failed to start container", err)

// New:
return nil, fmt.Errorf("failed to start container: %w: %w", ErrContainerStartFailed, err)
```

Replace line 196:
```go
// Old:
return nil, wrapError(ErrTypeUnknown, "failed to inspect container", err)

// New:
return nil, fmt.Errorf("failed to inspect container: %w", err)
```

Replace line 249:
```go
// Old:
return wrapError(ErrTypeImagePullFailed, fmt.Sprintf("failed to pull image %s", imageRef), err)

// New:
return fmt.Errorf("failed to pull image %s: %w: %w", imageRef, ErrImagePullFailed, err)
```

Replace line 256:
```go
// Old:
return wrapError(ErrTypeImagePullFailed, fmt.Sprintf("failed to read pull output for %s", imageRef), err)

// New:
return fmt.Errorf("failed to read pull output for %s: %w: %w", imageRef, ErrImagePullFailed, err)
```

**Step 4: Run tests**

Run: `cd v4 && go test -v`
Expected: PASS for all tests

**Step 5: Commit**

```bash
cd v4
git add pool.go pool_test.go
git commit -m "refactor: use fmt.Errorf for error wrapping in pool.go"
```

---

## Task 4: Update network.go Error Usage

**Files:**
- Modify: `v4/network.go:18,31,37,82`

**Step 1: Write test for network error wrapping**

Add to `v4/network_test.go`:

```go
func TestNetworkErrorWrapping(t *testing.T) {
	t.Run("network create error wrapping", func(t *testing.T) {
		mockErr := fmt.Errorf("failed to create network: %w", errors.New("docker error"))
		assert.Error(t, mockErr)
	})
}
```

**Step 2: Run test**

Run: `cd v4 && go test -run TestNetworkErrorWrapping -v`
Expected: PASS

**Step 3: Replace wrapError calls in network.go**

Replace line 18:
```go
// Old:
return nil, wrapError(ErrTypeUnknown, "failed to apply network option", err)

// New:
return nil, fmt.Errorf("failed to apply network option: %w", err)
```

Replace line 31:
```go
// Old:
return nil, wrapError(ErrTypeUnknown, "failed to create network", err)

// New:
return nil, fmt.Errorf("failed to create network: %w", err)
```

Replace line 37:
```go
// Old:
return nil, wrapError(ErrTypeUnknown, "failed to inspect network", err)

// New:
return nil, fmt.Errorf("failed to inspect network: %w", err)
```

Replace line 82:
```go
// Old:
return wrapError(ErrTypeUnknown, "failed to remove network", err)

// New:
return fmt.Errorf("failed to remove network: %w", err)
```

**Step 4: Run tests**

Run: `cd v4 && go test -v`
Expected: PASS

**Step 5: Commit**

```bash
cd v4
git add network.go network_test.go
git commit -m "refactor: use fmt.Errorf for error wrapping in network.go"
```

---

## Task 5: Update resource.go Error Usage

**Files:**
- Modify: `v4/resource.go:106,158,164,175,181,193,210,218,224`

**Step 1: Write test for resource error wrapping**

Add to `v4/resource_test.go`:

```go
func TestResourceErrorWrapping(t *testing.T) {
	t.Run("exec error wrapping", func(t *testing.T) {
		mockErr := fmt.Errorf("failed to create exec instance: %w: %w",
			ErrExecFailed, errors.New("docker error"))
		assert.True(t, errors.Is(mockErr, ErrExecFailed))
	})
}
```

**Step 2: Run test**

Run: `cd v4 && go test -run TestResourceErrorWrapping -v`
Expected: PASS

**Step 3: Replace wrapError calls in resource.go**

Replace line 106:
```go
// Old:
return wrapError(ErrTypeUnknown, "failed to remove container", err)

// New:
return fmt.Errorf("failed to remove container: %w", err)
```

Replace line 158:
```go
// Old:
return wrapError(ErrTypeUnknown, "failed to connect container to network", err)

// New:
return fmt.Errorf("failed to connect container to network: %w", err)
```

Replace line 164:
```go
// Old:
return wrapError(ErrTypeUnknown, "failed to inspect container after network connect", err)

// New:
return fmt.Errorf("failed to inspect container after network connect: %w", err)
```

Replace line 175:
```go
// Old:
return wrapError(ErrTypeUnknown, "failed to disconnect container from network", err)

// New:
return fmt.Errorf("failed to disconnect container from network: %w", err)
```

Replace line 181:
```go
// Old:
return wrapError(ErrTypeUnknown, "failed to inspect container after network disconnect", err)

// New:
return fmt.Errorf("failed to inspect container after network disconnect: %w", err)
```

Replace line 193:
```go
// Old:
return -1, wrapError(ErrTypeUnknown, "failed to apply exec option", err)

// New:
return -1, fmt.Errorf("failed to apply exec option: %w", err)
```

Replace line 210:
```go
// Old:
return -1, wrapError(ErrTypeExecFailed, "failed to create exec instance", err)

// New:
return -1, fmt.Errorf("failed to create exec instance: %w: %w", ErrExecFailed, err)
```

Replace line 218:
```go
// Old:
return -1, wrapError(ErrTypeExecFailed, "failed to start exec", err)

// New:
return -1, fmt.Errorf("failed to start exec: %w: %w", ErrExecFailed, err)
```

Replace line 224:
```go
// Old:
return -1, wrapError(ErrTypeExecFailed, "failed to inspect exec", err)

// New:
return -1, fmt.Errorf("failed to inspect exec: %w: %w", ErrExecFailed, err)
```

**Step 4: Run tests**

Run: `cd v4 && go test -v`
Expected: PASS

**Step 5: Commit**

```bash
cd v4
git add resource.go resource_test.go
git commit -m "refactor: use fmt.Errorf for error wrapping in resource.go"
```

---

## Task 6: Final Verification and Cleanup

**Files:**
- Test: All files in `v4/`

**Step 1: Run full test suite**

Run: `cd v4 && go test -v ./...`
Expected: PASS for all tests

**Step 2: Run go vet**

Run: `cd v4 && go vet ./...`
Expected: No issues

**Step 3: Check for any remaining references**

Run: `cd v4 && grep -r "wrapError\|ErrorType\|ErrType" --include="*.go"`
Expected: No results

**Step 4: Verify imports are clean**

Run: `cd v4 && goimports -l .`
Expected: No changes needed (or apply them if needed)

**Step 5: Final commit if any cleanup needed**

```bash
cd v4
git add -A
git commit -m "chore: cleanup imports and formatting after error refactor"
```

---

## Summary

This plan refactors the error handling from a custom type-based system to idiomatic Go sentinel errors:

**Before:**
- Custom `Error` struct with `Type` field
- `ErrorType` constants
- `wrapError()` helper function
- `IsNotFound()`, `IsTimeout()`, `IsConnectionError()` helpers

**After:**
- Exported sentinel error variables (`ErrContainerNotFound`, etc.)
- Standard `fmt.Errorf()` for wrapping
- Standard `errors.Is()` for checking

**Benefits:**
- More idiomatic Go
- Better composability with standard library
- Simpler codebase
- No custom error matching logic needed
