# Migration Guide: v3 to v4

This guide helps you migrate from dockertest v3 to v4.

## Why Migrate?

- **Security**: Removes vendored client with CVE vulnerabilities
- **Performance**: 2-3x faster tests with automatic container reuse
- **Modern Go**: Context support, functional options, sentinel errors
- **Lighter**: Official Moby client vs 6000+ lines of vendored code
- **Maintainable**: No vendored dependencies to update manually

## Key Differences

| v3                                                                                  | v4                                                                                                             |
| ----------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `pool, err := dockertest.NewPool("")`                                               | `pool := dockertest.NewPoolT(t, "")`                                                                           |
| `pool.MaxWait = time.Minute`                                                        | `dockertest.WithMaxWait(time.Minute)`                                                                          |
| `resource, err := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})` | `pool.RunT(t, "postgres", dockertest.WithTag("14"), dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}))` |
| `defer pool.Purge(resource)`                                                        | `resource.Cleanup(t)` or automatic via `pool.Cleanup()`                                                        |
| No context support                                                                  | Context throughout                                                                                             |
| Manual container reuse                                                              | Automatic container reuse by default                                                                           |
| Generic errors                                                                      | Sentinel errors (`ErrImagePullFailed`, `ErrTimeout`)                                                           |

## Breaking Changes

### Container Expiration

The `Expire` method from v3 is removed as it was not working as documented /
intended due to Docker API limitations.

### Import Path

```diff
-import "github.com/ory/dockertest/v3"
+import "github.com/ory/dockertest/v4"
```

### Pool Creation

```go
// v3
pool, err := dockertest.NewPool("")
pool.MaxWait = 2 * time.Minute

// v4 (in tests)
pool := dockertest.NewPoolT(t, "",
    dockertest.WithMaxWait(2 * time.Minute),
)

// v4 (outside tests)
ctx := context.Background()
pool, err := dockertest.NewPool(ctx, "",
    dockertest.WithMaxWait(2 * time.Minute),
)
defer pool.Close()
```

### Running Containers

```go
// v3
resource, err := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})
defer pool.Purge(resource)

// v4 (in tests)
resource := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
resource.Cleanup(t)

// v4 (outside tests)
resource, err := pool.Run(ctx, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
defer resource.Close(ctx)
```

### Functional Options

`RunWithOptions` is replaced by functional options:

```go
// v3
pool.RunWithOptions(&dockertest.RunOptions{
    Repository: "postgres",
    Tag:        "14",
    Env:        []string{"POSTGRES_DB=testdb"},
    Cmd:        []string{"postgres", "-c", "log_statement=all"},
})

// v4
pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_DB=testdb"}),
    dockertest.WithCmd([]string{"postgres", "-c", "log_statement=all"}),
)
```

### Cleanup Pattern

```go
// v3 - TestMain
func TestMain(m *testing.M) {
    pool, _ := dockertest.NewPool("")
    globalDB, _ := pool.Run("postgres", "14", nil)
    code := m.Run()
    pool.Purge(globalDB)
    os.Exit(code)
}

// v4 - TestMain with automatic cleanup
func TestMain(m *testing.M) {
    ctx := context.Background()
    pool, _ := dockertest.NewPool(ctx, "")
    code := m.Run()
    defer pool.Cleanup(ctx)
    os.Exit(code)
}
```

### Error Handling

Use sentinel errors with `errors.Is()`:

```go
resource, err := pool.Run(ctx, "postgres", dockertest.WithTag("14"))
if errors.Is(err, dockertest.ErrImagePullFailed) {
    // Handle image pull failure
}
if errors.Is(err, dockertest.ErrTimeout) {
    // Handle timeout
}
```

## Migration Steps

1. **Update dependencies:**

   ```bash
   go get github.com/ory/dockertest/v4
   go mod tidy
   ```

2. **Update imports:**

   ```bash
   find . -name "*.go" -exec sed -i '' 's|github.com/ory/dockertest/v3|github.com/ory/dockertest/v4|g' {} \;
   ```

3. **Add TestMain for cleanup:**

   ```go
   func TestMain(m *testing.M) {
       ctx := context.Background()
       pool, _ := dockertest.NewPool(ctx, "")
       code := m.Run()
       defer pool.Cleanup(ctx)
       os.Exit(code)
   }
   ```

4. **Convert API calls:** Replace `NewPool("")` with `NewPoolT(t, "")`, `Run()`
   with `RunT()`, and `pool.Purge()` with `resource.Cleanup(t)` or
   `pool.Cleanup(ctx)`. See Breaking Changes section for patterns.

5. **Test:** Run `go test ./...` to verify the migration.

## Common Patterns

### Single Container Test

```go
// v3
func TestDB(t *testing.T) {
    pool, _ := dockertest.NewPool("")
    resource, _ := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})
    defer pool.Purge(resource)
    port := resource.GetPort("5432/tcp")
}

// v4
func TestDB(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")
    db := pool.RunT(t, "postgres",
        dockertest.WithTag("14"),
        dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
    )
    db.Cleanup(t)
    port := db.GetPort("5432/tcp")
}
```

### Multiple Containers

```go
// v3
pool, _ := dockertest.NewPool("")
db, _ := pool.Run("postgres", "14", []string{"POSTGRES_PASSWORD=secret"})
defer pool.Purge(db)
cache, _ := pool.Run("redis", "7", nil)
defer pool.Purge(cache)

// v4
pool := dockertest.NewPoolT(t, "")
db := pool.RunT(t, "postgres",
    dockertest.WithTag("14"),
    dockertest.WithEnv([]string{"POSTGRES_PASSWORD=secret"}),
)
db.Cleanup(t)
cache := pool.RunT(t, "redis", dockertest.WithTag("7"))
cache.Cleanup(t)
```

### Automatic Container Reuse

v4 automatically reuses containers with the same `repo:tag` across tests:

```go
func TestUser(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")
    db := pool.RunT(t, "postgres", dockertest.WithTag("14")) // First call
}

func TestPost(t *testing.T) {
    pool := dockertest.NewPoolT(t, "")
    db := pool.RunT(t, "postgres", dockertest.WithTag("14")) // Reuses same container
}
```

### Custom Reuse ID (Different Configs)

```go
// Same image, different configurations using custom reuse IDs
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
```

## Troubleshooting

| Issue                      | Solution                                                         |
| -------------------------- | ---------------------------------------------------------------- |
| Container not found errors | Call `pool.Cleanup(ctx)` in TestMain instead of manual `Purge()` |
| Timeout errors             | Increase timeout: `dockertest.WithMaxWait(5 * time.Minute)`      |
| Context canceled errors    | Use `t.Context()` or `context.Background()` appropriately        |
| Image pull failures        | Check with `errors.Is(err, dockertest.ErrImagePullFailed)`       |

## Need Help?

- [API Documentation](https://pkg.go.dev/github.com/ory/dockertest/v4)
- [GitHub Issues](https://github.com/ory/dockertest/issues)
- [Examples Directory](examples)

## Migration Checklist

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
