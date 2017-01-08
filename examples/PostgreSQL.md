```go
var db *sql.DB
var err error
pool, err = dockertest.NewPool("")
if err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

resource, err := pool.Run("postgres", "9.6", []string{"POSTGRES_PASSWORD=secret", "POSTGRES_DB=" + database})
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