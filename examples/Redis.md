```go
var db *redis.Client
var err error
pool, err := dockertest.NewPool("")
if err != nil {
    log.Fatalf("Could not construct pool: %s", err)
}

err = pool.Client.Ping()
if err != nil {
    log.Fatalf("Could not connect to Docker: %s", err)
}

resource, err := pool.Run("redis", "3.2", nil)
if err != nil {
    log.Fatalf("Could not start resource: %s", err)
}

if err = pool.Retry(func() error {
    db = redis.NewClient(&redis.Options{
        Addr: fmt.Sprintf("localhost:%s", resource.GetPort("6379/tcp")),
    })

    return db.Ping().Err()
}); err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

// When you're done, kill and remove the container
if err = pool.Purge(resource); err != nil {
    log.Fatalf("Could not purge resource: %s", err)
}
```
