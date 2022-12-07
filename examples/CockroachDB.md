```go
var db *sql.DB
var err error
pool, err := dockertest.NewPool("")
if err != nil {
    log.Fatalf("Could not construct pool: %s", err)
}

err = pool.Client.Ping()
if err != nil {
    log.Fatalf("Could not connect to Docker: %s", err)
}

resource, err := pool.RunWithOptions(&dockertest.RunOptions{Repository: "cockroachdb/cockroach", Tag: "v19.2.4", Cmd: []string{"start-single-node", "--insecure"}})
if err != nil {
    log.Fatalf("Could not start resource: %s", err)
}

if err = pool.Retry(func() error {
    var err error
    db, err = sql.Open("postgres", fmt.Sprintf("postgresql://root@localhost:%s/defaultdb?sslmode=disable", resource.GetPort("26257/tcp")))
    if err != nil {
        return err
    }
    return db.Ping()
}); err != nil {
    log.Fatalf("Could not connect to cockroach container: %s", err)
}

// When you're done, kill and remove the container
if err = pool.Purge(resource); err != nil {
    log.Fatalf("Could not purge resource: %s", err)
}
```
