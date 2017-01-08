```go
var session *r.Session
var err error
pool, err = dockertest.NewPool("")
if err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

resource, err := pool.Run("rethinkdb", "2.3", []string{""})
if err != nil {
    log.Fatalf("Could not start resource: %s", err)
}

if err = pool.Retry(func() error {
    if session, err = r.Connect(r.ConnectOpts{Address: fmt.Sprintf("localhost:%s", resource.GetPort("28015/tcp")), Database: database}); err != nil {
        return err
    } else if _, err = r.DBCreate(database).RunWrite(session); err != nil {
        log.Printf("Database exists: %s", err)
        return err
    }

    for _, table := range tables {
        if _, err = r.TableCreate(table).RunWrite(session); err != nil {
            log.Printf("Could not create table: %s", err)
            return err
        }
    }

    time.Sleep(100 * time.Millisecond)
    return nil
}); err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

// When you're done, kill and remove the container
err = pool.Purge(resource)
```