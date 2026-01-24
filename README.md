# [ory.am](https://ory.am)/dockertest

[![Build Status](https://travis-ci.org/ory/dockertest.svg)](https://travis-ci.org/ory/dockertest?branch=master)
[![Coverage Status](https://coveralls.io/repos/github/ory/dockertest/badge.svg?branch=v4)](https://coveralls.io/github/ory/dockertest?branch=v4)

Use Docker to run your Go language integration tests against third party
services on **Microsoft Windows, Mac OSX and Linux**! Dockertest uses
[Docker](https://www.docker.com/toolbox) to spin up images on Windows and Mac
OSX as well.

Dockertest supports running any Docker Image from Docker Hub and Dockerfile.

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

**Table of Contents**

- [Why should I use Dockertest?](#why-should-i-use-dockertest)
- [Installing and using Dockertest v4](#installing-and-using-dockertest-v4)
  - [Installation](#installation)
  - [Quick Start](#quick-start)
  - [Key Features](#key-features)
  - [Examples](#examples)
- [Upgrading from v3 to v4](#upgrading-from-v3-to-v4)
  - [Breaking Changes](#breaking-changes)
  - [Migration Examples](#migration-examples)
  - [Common Patterns](#common-patterns)
- [Using Dockertest v3 (Legacy)](#using-dockertest-v3-legacy)
- [Troubleshoot & FAQ](#troubleshoot--faq)
  - [Out of disk space](#out-of-disk-space)
  - [Removing old containers](#removing-old-containers)
- [Running in CI](#running-in-ci)
  - [GitHub Actions](#github-actions)
  - [GitLab CI](#gitlab-ci)
  - [Remote Docker](#remote-docker)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## Why should I use Dockertest?

When developing applications, it is often necessary to use services that talk to
a database system. Unit testing these services can be cumbersome because mocking
database/DBAL is strenuous. Making slight changes to the schema implies
rewriting at least some, if not all mocks. The same goes for API changes in the
DBAL.

To avoid this, it is smarter to test these specific services against a real
database that is destroyed after testing. Docker is the perfect system for
running integration tests as you can spin up containers in a few seconds and
kill them when the test completes.

The Dockertest library provides easy to use commands for spinning up Docker
containers and using them for your tests.

## Installation

```bash
go get github.com/ory/dockertest/v4
```

## Quick Start

```go
package myapp_test

import (
    "context"
    "os"
    "testing"

    dockertest "github.com/ory/dockertest/v4"
)

func TestPostgres(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")

    db := pool.RunT(t, "postgres",
        dockertest.WithTag("14"),
        dockertest.WithEnv([]string{
            "POSTGRES_PASSWORD=secret",
            "POSTGRES_DB=testdb",
        }),
    )
    defer db.Cleanup(t)

    // Use db.GetHostPort("5432/tcp") to connect
    hostPort := db.GetHostPort("5432/tcp")
    // Connect to postgres://postgres:secret@hostPort/testdb
}

func TestMain(m *testing.M) {
    ctx := context.Background()
    pool, _ := dockertest.NewPool(ctx, "")
    defer pool.Cleanup(ctx)
    os.Exit(m.Run())
}
```

## Migration from v3

**Version 4 introduces automatic container reuse, making tests significantly
faster by reusing containers across test runs. Additionally, a lightweight
docker client is used which reduces third party dependencies significantly.**

See [docs/migration-v3-to-v4.md](../docs/migration-v3-to-v4.md) for the complete
migration guide.

### Key Differences

| v3                                                                                  | v4                                                                                                             |
| ----------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `pool, err := dockertest.NewPool("")`                                               | `pool := dockertest.NewPoolT(t, "")`                                                                           |
| `pool.MaxWait = time.Minute`                                                        | `dockertest.WithMaxWait(time.Minute)`                                                                          |
| `resource, err := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})` | `pool.RunT(t, "postgres", dockertest.WithTag("14"), dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}))` |
| `defer pool.Purge(resource)`                                                        | `defer resource.Cleanup(t)` or automatic via `pool.Cleanup()`                                                  |
| No context support                                                                  | Context throughout                                                                                             |

## API Overview

### Pool Creation

```go
// For tests - auto-cleanup with t.Cleanup()
pool := dockertest.NewPoolT(t, "")

// With options
pool := dockertest.NewPoolT(t, "",
    dockertest.WithMaxWait(2*time.Minute),
)

// For non-test code - requires manual Close()
ctx := context.Background()
pool, err := dockertest.NewPool(ctx, "")
if err != nil {
    panic(err)
}
defer pool.Close()
```

### Running Containers

```go
// Test helper - fails test on error
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
    dockertest.WithCmd([]string{"postgres", "-c", "log_statement=all"}),
)

// With error handling
resource, err := pool.Run(ctx, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
if err != nil {
    panic(err)
}
```

### Container Configuration

Customize container settings with configuration options:

```go
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithUser("postgres"),
    dockertest.WithWorkingDir("/var/lib/postgresql/data"),
    dockertest.WithLabels(map[string]string{
        "test":    "integration",
        "service": "database",
    }),
    dockertest.WithHostname("test-db"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
```

Available configuration options:

- `WithUser(user string)` - Set the user to run commands as (supports "user" or
  "user:group")
- `WithWorkingDir(dir string)` - Set the working directory
- `WithLabels(labels map[string]string)` - Add labels to the container
- `WithHostname(hostname string)` - Set the container hostname
- `WithEnv(env []string)` - Set environment variables
- `WithCmd(cmd []string)` - Override the default command
- `WithEntrypoint(entrypoint []string)` - Override the default entrypoint

For advanced configuration, use `WithContainerConfig`:

```go
stopTimeout := 30
resource := pool.RunT(t, "app",
    dockertest.WithContainerConfig(func(cfg *container.Config) {
        cfg.StopTimeout = &stopTimeout
        cfg.StopSignal = "SIGTERM"
        cfg.Healthcheck = &container.HealthConfig{
            Test:     []string{"CMD", "curl", "-f", "http://localhost/health"},
            Interval: 10 * time.Second,
            Timeout:  5 * time.Second,
            Retries:  3,
        }
    }),
)
```

### Container Reuse

Containers are automatically reused based on `repository:tag`:

```go
// First test creates container
r1 := pool.RunT(t, "postgres", dockertest.WithTag("14"))

// Second test reuses the same container (2-3x faster)
r2 := pool.RunT(t, "postgres", dockertest.WithTag("14"))

// r1 and r2 point to the same container
```

Disable reuse if needed:

```go
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithoutReuse(), // Always create new container
)
```

### Getting Connection Info

```go
resource := pool.RunT(t, "postgres", dockertest.WithTag("14"))

// Get host port (e.g., "127.0.0.1:54320")
hostPort := resource.GetHostPort("5432/tcp")

// Get just the port (e.g., "54320")
port := resource.GetPort("5432/tcp")

// Get just the IP (e.g., "127.0.0.1")
ip := resource.GetBoundIP("5432/tcp")

// Get container ID
id := resource.ID()
```

### Cleanup

```go
// In tests - cleanup when test finishes
resource.Cleanup(t)

// Manual cleanup
err := resource.Close(ctx)

// Test helper - fails test on error
resource.CloseT(t)

// Cleanup all containers (in TestMain)
pool.Cleanup(ctx)
```

### Error Handling

```go
resource, err := pool.Run(ctx, "postgres", dockertest.WithTag("14"))
if errors.Is(err, dockertest.ErrImagePullFailed) {
    // Image could not be pulled
}
if errors.Is(err, dockertest.ErrContainerCreateFailed) {
    // Container creation failed
}
if errors.Is(err, dockertest.ErrContainerStartFailed) {
    // Container start failed
}
```

## Examples

See the [examples directory](./examples/) for complete examples with:

- PostgreSQL
- Redis
- More coming soon

## License

Apache 2.0
