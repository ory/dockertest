<h1 align="center"><img src="./docs/images/banner_dockertest.png" alt="ORY Dockertest"></h1>

[![CI](https://github.com/ory/dockertest/actions/workflows/test.yml/badge.svg)](https://github.com/ory/dockertest/actions/workflows/test.yml)

Use Docker to run your Go integration tests against third party services on
**Windows, macOS, and Linux**!

Dockertest supports running any Docker image from Docker Hub or from a
Dockerfile.

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

- [Why should I use Dockertest?](#why-should-i-use-dockertest)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Migration from v3](#migration-from-v3)
- [API overview](#api-overview)
  - [Pool creation](#pool-creation)
  - [Running containers](#running-containers)
  - [Container configuration](#container-configuration)
  - [Container reuse](#container-reuse)
  - [Getting connection info](#getting-connection-info)
  - [Waiting for readiness](#waiting-for-readiness)
  - [Executing commands](#executing-commands)
  - [Container logs](#container-logs)
  - [Building from Dockerfile](#building-from-dockerfile)
  - [Networks](#networks)
  - [Cleanup](#cleanup)
  - [Error handling](#error-handling)
- [Examples](#examples)
- [Troubleshoot & FAQ](#troubleshoot--faq)
  - [Out of disk space](#out-of-disk-space)
- [Running in CI](#running-in-ci)
  - [GitHub Actions](#github-actions)
  - [GitLab CI](#gitlab-ci)
    - [Shared runners](#shared-runners)
    - [Custom (group) runners](#custom-group-runners)

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
> Dockertest v4 is not yet finalized and may still receive breaking changes
> before the stable release.

## Installation

```bash
go get github.com/ory/dockertest/v4
```

## Quick Start

```go
package myapp_test

import (
    "testing"
    "time"

    dockertest "github.com/ory/dockertest/v4"
)

func TestPostgres(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")

    // Container is automatically reused across test runs based on "postgres:14".
    postgres := pool.RunT(t, "postgres",
        dockertest.WithTag("14"),
        dockertest.WithEnv([]string{
            "POSTGRES_PASSWORD=secret",
            "POSTGRES_DB=testdb",
        }),
    )

    hostPort := postgres.GetHostPort("5432/tcp")
    // Connect to postgres://postgres:secret@hostPort/testdb

    // Wait for PostgreSQL to be ready
    err := pool.Retry(t.Context(), 30*time.Second, func() error {
        // try connecting...
        return nil
    })
    if err != nil {
        t.Fatalf("Could not connect: %v", err)
    }
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

// With a custom Docker client
pool := dockertest.NewPoolT(t, "",
    dockertest.WithMobyClient(myClient),
)

// For non-test code - requires manual Close()
ctx := context.Background()
pool, err := dockertest.NewPool(ctx, "")
if err != nil {
    panic(err)
}
defer pool.Close(ctx)
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

> See [Cleanup](#cleanup) for container lifecycle management.

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

- `WithTag(tag string)` - Set the image tag (default: `"latest"`)
- `WithEnv(env []string)` - Set environment variables
- `WithCmd(cmd []string)` - Override the default command
- `WithEntrypoint(entrypoint []string)` - Override the default entrypoint
- `WithUser(user string)` - Set the user to run commands as (supports "user" or
  "user:group")
- `WithWorkingDir(dir string)` - Set the working directory
- `WithLabels(labels map[string]string)` - Add labels to the container
- `WithHostname(hostname string)` - Set the container hostname
- `WithName(name string)` - Set the container name
- `WithMounts(binds []string)` - Set bind mounts ("host:container" or
  "host:container:mode")
- `WithPortBindings(bindings network.PortMap)` - Set explicit port bindings
- `WithReuseID(id string)` - Set a custom reuse key (default:
  `"repository:tag"`)
- `WithoutReuse()` - Disable container reuse for this run
- `WithContainerConfig(modifier func(*container.Config))` - Modify the
  container config directly
- `WithHostConfig(modifier func(*container.HostConfig))` - Modify the host
  config (port bindings, volumes, restart policy, memory/CPU limits)

For advanced container configuration, use `WithContainerConfig`:

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

For host-level configuration, use `WithHostConfig`:

```go
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithHostConfig(func(hc *container.HostConfig) {
        hc.RestartPolicy = container.RestartPolicy{
            Name:              container.RestartPolicyOnFailure,
            MaximumRetryCount: 3,
        }
    }),
)
```

### Container reuse

> [!WARNING]
>
> Do not use `resource.Cleanup(t)` on reused containers. Because reused
> containers are shared across tests, cleaning up one reference will remove the
> container for all other tests that depend on it. Only use `pool.Close(ctx)`
> in `TestMain` to clean up reused containers after all tests have finished.

Containers are automatically reused based on `repository:tag`:

```go
// First test creates container
r1 := pool.RunT(t, "postgres", dockertest.WithTag("14"))

// Second test reuses the same container
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

// Get host:port (e.g., "127.0.0.1:54320")
hostPort := resource.GetHostPort("5432/tcp")

// Get just the port (e.g., "54320")
port := resource.GetPort("5432/tcp")

// Get just the IP (e.g., "127.0.0.1")
ip := resource.GetBoundIP("5432/tcp")

// Get container ID
id := resource.ID()
```

### Waiting for readiness

Use `pool.Retry` to wait for a container to become ready:

```go
err := pool.Retry(t.Context(), 30*time.Second, func() error {
    return db.Ping()
})
if err != nil {
    t.Fatalf("Container not ready: %v", err)
}
```

If timeout is 0, `pool.MaxWait` (default 60s) is used. The retry interval is
fixed at 1 second.

For more control, use the package-level functions:

```go
// Fixed interval retry
err := dockertest.Retry(ctx, 30*time.Second, 500*time.Millisecond, func() error {
    return db.Ping()
})

// Exponential backoff retry
err := dockertest.RetryWithBackoff(ctx,
    30*time.Second,       // timeout
    100*time.Millisecond, // initial interval
    5*time.Second,        // max interval
    func() error {
        return db.Ping()
    },
)
```

### Executing commands

Run commands inside a running container:

```go
result, err := resource.Exec(ctx, []string{"pg_isready", "-U", "postgres"})
if err != nil {
    t.Fatal(err)
}
if result.ExitCode != 0 {
    t.Fatalf("command failed: %s", result.StdErr)
}
t.Log(result.StdOut)
```

### Container logs

```go
logs, err := resource.Logs(ctx)
if err != nil {
    t.Fatal(err)
}
t.Log(logs)
```

### Building from Dockerfile

Build a Docker image from a Dockerfile and run it:

```go
version := "1.0.0"
resource, err := pool.BuildAndRun(ctx, "myapp:test",
    &dockertest.BuildOptions{
        ContextDir: "./testdata",
        Dockerfile: "Dockerfile.test",
        BuildArgs:  map[string]*string{"VERSION": &version},
    },
    dockertest.WithEnv([]string{"APP_ENV=test"}),
)
if err != nil {
    t.Fatal(err)
}
```

`BuildAndRunT` is the test helper variant:

```go
resource := pool.BuildAndRunT(t, "myapp:test",
    &dockertest.BuildOptions{
        ContextDir: "./testdata",
    },
)
```

### Networks

Create Docker networks for container-to-container communication:

```go
net := pool.CreateNetworkT(t, "my-network", nil)

// Connect a container
err := resource.ConnectToNetwork(ctx, net)

// Get the container's IP in the network
ip := resource.GetIPInNetwork(net)

// Disconnect
err := resource.DisconnectFromNetwork(ctx, net)
```

With custom options:

```go
net, err := pool.CreateNetwork(ctx, "my-network", &dockertest.NetworkCreateOptions{
    Driver:   "bridge",
    Internal: true,
})
```

### Cleanup

> [!WARNING]
>
> Do not use `resource.Cleanup(t)` or `resource.CloseT(t)` on **reused**
> containers. Because reused containers are shared across tests, cleaning up one
> reference removes the container for all other tests. Use `pool.Close(ctx)` in
> `TestMain` to clean up reused containers after all tests finish.

Use `Cleanup(t)` for **non-reused** containers — it registers `t.Cleanup` and
logs errors without failing the test:

```go
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithoutReuse(),
)
resource.Cleanup(t) // removed when t finishes
```

Use `CloseT(t)` when you need **immediate** cleanup and want a hard failure on
error (calls `t.Fatalf`):

```go
resource.CloseT(t) // removes now, fails test on error
```

Use `Close(ctx)` in non-test code for manual cleanup:

```go
err := resource.Close(ctx)
```

Use `pool.Close(ctx)` to clean up **all** resources (reused and non-reused),
typically in `TestMain`:

```go
func TestMain(m *testing.M) {
    ctx := context.Background()
    pool, _ := dockertest.NewPool(ctx, "")
    code := m.Run()
    pool.Close(ctx)
    os.Exit(code)
}
```

### Error handling

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
if errors.Is(err, dockertest.ErrClientClosed) {
    // Pool or client has been closed
}
```

## Examples

See the [examples directory](./examples) for complete examples.

## Troubleshoot & FAQ

### Out of disk space

Try cleaning up unused containers, images, and volumes:

```bash
docker system prune -f
```

## Running in CI

### GitHub Actions

Docker is available by default on GitHub Actions `ubuntu-latest` runners, so no
extra services are needed:

```yaml
name: Test with Docker

on: [push]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: "1.24"

      - run: go test -v ./...
```

### GitLab CI

#### Shared runners

Add the Docker dind service to your job which starts in a sibling container.
The database will be available on host `docker`. Your app should be able to
change the database host through an environment variable.

```yaml
stages:
  - test
go-test:
  stage: test
  image: golang:1.24
  services:
    - docker:dind
  variables:
    DOCKER_HOST: tcp://docker:2375
    DOCKER_DRIVER: overlay2
    DOCKER_TLS_CERTDIR: ""
    YOUR_APP_DB_HOST: docker
  script:
    - go test ./...
```

In your `pool.Retry` callback, use `$YOUR_APP_DB_HOST` instead of localhost
when connecting to the database.

#### Custom (group) runners

GitLab runner can be run in docker executor mode to save compatibility with
shared runners:

```shell
gitlab-runner register -n \
 --url https://gitlab.com/ \
 --registration-token $YOUR_TOKEN \
 --executor docker \
 --description "My Docker Runner" \
 --docker-image "docker:27" \
 --docker-privileged
```

The `DOCKER_TLS_CERTDIR: ""` variable in the example above tells the Docker
daemon to start on port 2375 over HTTP (TLS disabled).
