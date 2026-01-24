# Migration Guide: v3 to v4

This guide helps you migrate from dockertest v3 to v4.

## Why Migrate?

- **Security**: Removes vendored client with CVE vulnerabilities
- **Performance**: 2-3x faster tests with automatic container reuse
- **Modern Go**: Context support, functional options, sentinel errors
- **Lighter**: Official Moby client vs 6000+ lines of vendored code
- **Maintainable**: No vendored dependencies to update manually

## Breaking Changes

### Import Path

```diff
-import "github.com/ory/dockertest/v3"
+import "github.com/ory/dockertest/v4"
```

### Pool Creation

v3:

```go
pool, err := dockertest.NewPool("")
if err != nil {
    panic(err)
}
```

v4 (test helper):

```go
pool := dockertest.NewPoolT(t, "")
// Auto-cleanup via t.Cleanup()
```

v4 (with context and error handling):

```go
ctx := context.Background()
pool, err := dockertest.NewPool(ctx, "")
if err != nil {
    panic(err)
}
defer pool.Close()
```

### Configuration

v3:

```go
pool, err := dockertest.NewPool("")
pool.MaxWait = 2 * time.Minute
```

v4:

```go
pool := dockertest.NewPoolT(t, "",
    dockertest.WithMaxWait(2 * time.Minute),
)
```

### Running Containers

v3:

```go
resource, err := pool.Run("postgres", "14", []string{
    "POSTGRES_PASSWORD=secret",
})
if err != nil {
    panic(err)
}
```

v4 (test helper):

```go
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
```

v4 (with error handling):

```go
resource, err := pool.Run(ctx, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
if err != nil {
    panic(err)
}
```

### RunWithOptions

v3:

```go
resource, err := pool.RunWithOptions(&dockertest.RunOptions{
    Repository: "postgres",
    Tag:        "14",
    Env: []string{
        "POSTGRES_PASSWORD=secret",
        "POSTGRES_DB=testdb",
    },
    Cmd: []string{"postgres", "-c", "log_statement=all"},
})
```

v4:

```go
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{
        "POSTGRES_PASSWORD=secret",
        "POSTGRES_DB=testdb",
    }),
    dockertest.WithCmd([]string{"postgres", "-c", "log_statement=all"}),
)
```

### Cleanup

v3:

```go
resource, err := pool.Run("postgres", "14", nil)
defer pool.Purge(resource)
```

v4 (individual resource):

```go
resource := pool.RunT(t, "postgres", dockertest.WithTag("14"))
defer resource.Cleanup(t) // Cleanup when test completes
```

v4 (all resources in TestMain):

```go
func TestMain(m *testing.M) {
    ctx := context.Background()
    pool, _ := dockertest.NewPool(ctx, "")
    defer pool.Cleanup(ctx) // Cleanup all containers
    os.Exit(m.Run())
}
```

### Port Access

No change:

```go
// v3 and v4 both use:
port := resource.GetPort("5432/tcp")
hostPort := resource.GetHostPort("5432/tcp")
ip := resource.GetBoundIP("5432/tcp")
```

### Error Handling

v3:

```go
resource, err := pool.Run("postgres", "14", nil)
if err != nil {
    return err
}
```

v4 (with sentinel errors):

```go
resource, err := pool.Run(ctx, "postgres", dockertest.WithTag("14"))
if errors.Is(err, dockertest.ErrImagePullFailed) {
    // Handle image pull failure specifically
}
if errors.Is(err, dockertest.ErrTimeout) {
    // Handle timeout
}
if err != nil {
    return err
}
```

## Step-by-Step Migration

### 1. Update go.mod

```bash
go get github.com/ory/dockertest/v4
go mod tidy
```

### 2. Update imports

Find and replace across all Go files:

```bash
find . -name "*.go" -exec sed -i '' 's|github.com/ory/dockertest/v3|github.com/ory/dockertest/v4|g' {} \;
```

### 3. Add TestMain for cleanup

Add this to your test package (if not already present):

```go
func TestMain(m *testing.M) {
    ctx := context.Background()
    pool, _ := dockertest.NewPool(ctx, "")
    defer pool.Cleanup(ctx)
    os.Exit(m.Run())
}
```

### 4. Convert Pool creation

Before:

```go
pool, err := dockertest.NewPool("")
if err != nil {
    panic(err)
}
pool.MaxWait = 2 * time.Minute
```

After (in tests):

```go
pool := dockertest.NewPoolT(t, "",
    dockertest.WithMaxWait(2 * time.Minute),
)
```

After (outside tests):

```go
ctx := context.Background()
pool, err := dockertest.NewPool(ctx, "",
    dockertest.WithMaxWait(2 * time.Minute),
)
if err != nil {
    panic(err)
}
defer pool.Close()
```

### 5. Convert Run calls

Before:

```go
resource, err := pool.Run("postgres", "14", []string{
    "POSTGRES_PASSWORD=secret",
})
if err != nil {
    panic(err)
}
defer pool.Purge(resource)
```

After (in tests):

```go
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
defer resource.Cleanup(t)
```

After (outside tests):

```go
ctx := context.Background()
resource, err := pool.Run(ctx, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
if err != nil {
    panic(err)
}
defer resource.Close(ctx)
```

### 6. Convert RunWithOptions

Before:

```go
resource, err := pool.RunWithOptions(&dockertest.RunOptions{
    Repository: "postgres",
    Tag:        "14",
    Env:        []string{"POSTGRES_PASSWORD=secret"},
    Cmd:        []string{"postgres", "-c", "log_statement=all"},
})
```

After:

```go
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
    dockertest.WithCmd([]string{"postgres", "-c", "log_statement=all"}),
)
```

### 7. Update error handling

Before:

```go
if err != nil {
    return err
}
```

After (with specific error checking):

```go
if errors.Is(err, dockertest.ErrImagePullFailed) {
    // Handle specifically
}
if err != nil {
    return err
}
```

### 8. Test your changes

```bash
go test ./...
```

## Common Patterns

### Pattern: PostgreSQL Integration Test

**Before (v3):**

```go
func TestPostgres(t *testing.T) {
    pool, err := dockertest.NewPool("")
    if err != nil {
        t.Fatalf("Could not connect to docker: %s", err)
    }

    resource, err := pool.Run("postgres", "14", []string{
        "POSTGRES_PASSWORD=secret",
        "POSTGRES_DB=testdb",
    })
    if err != nil {
        t.Fatalf("Could not start resource: %s", err)
    }
    defer pool.Purge(resource)

    hostPort := resource.GetPort("5432/tcp")
    // Use hostPort...
}
```

**After (v4):**

```go
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

    hostPort := db.GetHostPort("5432/tcp")
    // Use hostPort...
}
```

### Pattern: Redis with Custom Configuration

**Before (v3):**

```go
pool, err := dockertest.NewPool("")
if err != nil {
    panic(err)
}
pool.MaxWait = time.Minute

resource, err := pool.RunWithOptions(&dockertest.RunOptions{
    Repository: "redis",
    Tag:        "7-alpine",
    Cmd:        []string{"redis-server", "--requirepass", "secret"},
})
if err != nil {
    panic(err)
}
defer pool.Purge(resource)

port := resource.GetPort("6379/tcp")
```

**After (v4):**

```go
pool := dockertest.NewPoolT(t, "",
    dockertest.WithMaxWait(time.Minute),
)

resource := pool.RunT(t, "redis",
    dockertest.WithTag("7-alpine"),
    dockertest.WithCmd([]string{"redis-server", "--requirepass", "secret"}),
)
defer resource.Cleanup(t)

port := resource.GetPort("6379/tcp")
```

### Pattern: Multiple Containers

**Before (v3):**

```go
pool, err := dockertest.NewPool("")
if err != nil {
    panic(err)
}

db, err := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})
if err != nil {
    panic(err)
}
defer pool.Purge(db)

cache, err := pool.Run("redis", "7", nil)
if err != nil {
    panic(err)
}
defer pool.Purge(cache)
```

**After (v4):**

```go
pool := dockertest.NewPoolT(t, "")

db := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
defer db.Cleanup(t)

cache := pool.RunT(t, "redis", dockertest.WithTag("7"))
defer cache.Cleanup(t)
```

### Pattern: Container Reuse Across Tests

**Before (v3):**

```go
// No built-in reuse - must implement manually
var globalDB *dockertest.Resource

func TestMain(m *testing.M) {
    pool, _ := dockertest.NewPool("")
    var err error
    globalDB, err = pool.Run("postgres", "14", nil)
    if err != nil {
        panic(err)
    }
    code := m.Run()
    pool.Purge(globalDB)
    os.Exit(code)
}
```

**After (v4):**

```go
// Automatic reuse - just call RunT with same repo:tag
func TestUser(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")
    db := pool.RunT(t, "postgres", dockertest.WithTag("14")) // Reused
    // Test user logic
}

func TestPost(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")
    db := pool.RunT(t, "postgres", dockertest.WithTag("14")) // Same container!
    // Test post logic
}

func TestMain(m *testing.M) {
    ctx := context.Background()
    pool, _ := dockertest.NewPool(ctx, "")
    defer pool.Cleanup(ctx) // Cleanup all containers
    os.Exit(m.Run())
}
```

### Pattern: Custom Reuse ID

**Before (v3):**

```go
// Not available - must implement manually
```

**After (v4):**

```go
// Use custom reuse ID for same image with different configs
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
// db1 and db2 are different containers
```

## Troubleshooting

### Container not found after migration

**Issue:** Tests fail with "container not found" errors.

**Solution:** Ensure you're calling `pool.Cleanup(ctx)` in TestMain, not relying
on v3's manual `Purge()`.

### Timeout errors

**Issue:** Operations timeout in v4 but worked in v3.

**Solution:** Adjust MaxWait using functional options:

```go
pool := dockertest.NewPoolT(t, "",
    dockertest.WithMaxWait(5 * time.Minute),
)
```

### Context canceled errors

**Issue:** Getting "context canceled" errors.

**Solution:** Ensure you're passing the correct context. In tests, use
`t.Context()` or `context.Background()`:

```go
// In tests
resource := pool.RunT(t, "postgres", ...) // Uses t.Context() automatically

// Outside tests
ctx := context.Background()
resource, err := pool.Run(ctx, "postgres", ...)
```

### Image pull failures

**Issue:** Images fail to pull.

**Solution:** Check error with `errors.Is()`:

```go
resource, err := pool.Run(ctx, "postgres", dockertest.WithTag("14"))
if errors.Is(err, dockertest.ErrImagePullFailed) {
    // Check network, Docker daemon, image name
    log.Printf("Image pull failed: %v", err)
}
```

## Need Help?

- [API Documentation](https://pkg.go.dev/github.com/ory/dockertest/v4)
- [GitHub Issues](https://github.com/ory/dockertest/issues)
- [Examples Directory](examples)

## Checklist

- [ ] Updated go.mod with v4 dependency
- [ ] Updated all imports to v4
- [ ] Added TestMain with Cleanup()
- [ ] Converted NewPool to NewPoolT or NewPool with context
- [ ] Converted Run to RunT or Run with context
- [ ] Updated RunWithOptions to use functional options
- [ ] Replaced pool.Purge with resource.Cleanup or pool.Cleanup
- [ ] Updated error handling to use errors.Is()
- [ ] Tested all changes
- [ ] Verified container cleanup works correctly

## Benefits After Migration

After migrating to v4, you'll enjoy:

1. **Faster Tests**: Automatic container reuse cuts test time by 2-3x
2. **Better Errors**: Sentinel errors make debugging easier
3. **Cleaner Code**: Functional options are more readable than structs
4. **Context Support**: Proper timeout and cancellation handling
5. **Security**: No vendored dependencies with CVEs
6. **Future-Proof**: Built on official Moby client

Welcome to dockertest v4!
