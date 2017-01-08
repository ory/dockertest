```go
var db *sql.DB
var err error
pool, err = dockertest.NewPool("")
pool.MaxWait = time.Minute * 2
if err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

resource, err := pool.Run("mysql", "5.7", []string{"MYSQL_ROOT_PASSWORD=secret"})
if err != nil {
    log.Fatalf("Could not start resource: %s", err)
}

if err = pool.Retry(func() error {
    var err error
    db, err = sql.Open("mysql", fmt.Sprintf("root:secret@(localhost:%s)/mysql?parseTime=true", resource.GetPort("3306/tcp")))
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