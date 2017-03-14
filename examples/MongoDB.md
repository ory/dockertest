```go
var db *mgo.Session
var err error

pool, err = dockertest.NewPool("")
if err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

resource, err := pool.Run("mongo", "3.0", nil)
if err != nil {
    log.Fatalf("Could not start resource: %s", err)
}

// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
if err := pool.Retry(func() error {
    var err error
    db, err = mgo.Dial(fmt.Sprintf("localhost:%s", resource.GetPort("27017/tcp")))
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
