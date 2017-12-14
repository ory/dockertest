`./db/image/Dockerfile`
```Dockerfile
FROM postgres:latest

# Add your customizations here
```

`./db_test.go`
```go
pool, err := dockertest.NewPool("")
if err != nil {
	log.Fatalf("Could not connect to docker: %s", err)
}

// Build and run the given Dockerfile
resource, err := pool.BuildAndRun("my-postgres-test-image", "./db/image/Dockerfile", []string{})
if err != nil {
	log.Fatalf("Could not start resource: %s", err)
}

if err = pool.Retry(func() error {
    var err error
    db, err = sql.Open("postgres", fmt.Sprintf("postgres://postgres:secret@localhost:%s/%s?sslmode=disable", resource.GetPort("5432/tcp"), database))
    if err != nil {
        return err
    }
    return db.Ping()
}); err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

// When you're done, kill and remove the container
err = pool.Purge(resource)
```