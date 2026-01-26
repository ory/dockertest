<h1 align="center"><img src="./docs/images/banner_dockertest.png" alt="ORY Dockertest"></h1>

[![Build Status](https://travis-ci.org/ory/dockertest.svg)](https://travis-ci.org/ory/dockertest?branch=master)
[![Coverage Status](https://coveralls.io/repos/github/ory/dockertest/badge.svg?branch=v4)](https://coveralls.io/github/ory/dockertest?branch=v4)

Use Docker to run your Go language integration tests against third party
services on **Microsoft Windows, Mac OSX and Linux**! Dockertest uses
[Docker](https://www.docker.com/toolbox) to spin up images on Windows and Mac
OSX.

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

> [!WARNING]
>
> Version 4 of this is not yet finalized and may still receive breaking changes
> before the stable release.

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
    db.Cleanup(t)

    // Use db.GetHostPort("5432/tcp") to connect
    hostPort := db.GetHostPort("5432/tcp")
    // Connect to postgres://postgres:secret@hostPort/testdb
}

func TestMain(m *testing.M) {
    // Note: TestMain doesn't have testing.T, so we create a context here
    // In regular tests, always use t.Context() instead
    ctx := context.Background()
    pool, _ := dockertest.NewPool(ctx, "")
    code := m.Run()
    defer pool.Cleanup(ctx)
    os.Exit(code)
}
```

## Migration from v3

Version 4 introduces automatic container reuse, making tests significantly
faster by reusing containers across test runs. Additionally, a lightweight
docker client is used which reduces third party dependencies significantly.

See [UPGRADE.md](UPGRADE.md) for the complete migration guide.

## API overview

View the Go
[API documentation](https://pkg.go.dev/github.com/ory/dockertest/v4).

### Pool creation

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

### Running containers

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

### Container configuration

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

### Container reuse

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

### Getting connection info

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

See the [examples directory](./examples) for complete examples.

## Troubleshoot & FAQ

### Out of disk space

Try cleaning up the images with
[docker-cleanup-volumes](https://github.com/chadoe/docker-cleanup-volumes).

## Running dockertest in Gitlab CI

### How to run dockertest on shared gitlab runners?

You should add docker dind service to your job which starts in sibling
container. That means database will be available on host `docker`.  
You app should be able to change db host through environment variable.

Here is the simple example of `gitlab-ci.yml`:

```yaml
stages:
  - test
go-test:
  stage: test
  image: golang:1.15
  services:
    - docker:dind
  variables:
    DOCKER_HOST: tcp://docker:2375
    DOCKER_DRIVER: overlay2
    YOUR_APP_DB_HOST: docker
  script:
    - go test ./...
```

Plus in the `pool.Retry` method that checks for connection readiness, you need
to use `$YOUR_APP_DB_HOST` instead of localhost.

### How to run dockertest on group(custom) gitlab runners?

Gitlab runner can be run in docker executor mode to save compatibility with
shared runners.  
Here is the simple register command:

```shell script
gitlab-runner register -n \
 --url https://gitlab.com/ \
 --registration-token $YOUR_TOKEN \
 --executor docker \
 --description "My Docker Runner" \
 --docker-image "docker:19.03.12" \
 --docker-privileged
```

You only need to instruct docker dind to start with disabled tls.  
Add variable `DOCKER_TLS_CERTDIR: ""` to `gitlab-ci.yml` above. It will tell
docker daemon to start on 2375 port over http.

## Running Dockertest using GitHub actions

```yaml
name: Test with Docker

on: [push]

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      dind:
        image: docker:23.0-rc-dind-rootless
        ports:
          - 2375:2375
    steps:
      - name: Checkout code
        uses: actions/checkout@v2

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: "1.21"

      - name: Test with Docker
        run: go test -v ./...
```
