```go
var db *mgo.Session
var err error

pool, err = dockertest.NewPool("")
if err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

options := &dockertest.RunOptions{
    Repository: "cassandra",
    Tag:        "latest",
    Mounts:     []string{"/tmp/local-cassandra:/etc/cassandra"},
}

resource, err := pool.RunWithOptions(options)
if err != nil {
    log.Fatalf("Could not start resource: %s", err)
}

retURL = fmt.Sprintf("localhost:%s", resource.GetPort("9042/tcp"))
port, _ := strconv.Atoi(resource.GetPort("9042/tcp"))

// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
if err := pool.Retry(func() error {
    clusterConfig := gocql.NewCluster(retURL)
    clusterConfig.Authenticator = gocql.PasswordAuthenticator{
        Username: "cassandra",
        Password: "cassandra",
    }
    clusterConfig.ProtoVersion = 4
    clusterConfig.Port = port
    log.Printf("%v", clusterConfig.Port)

    session, err := clusterConfig.CreateSession()
    if err != nil {
        return fmt.Errorf("error creating session: %s", err)
    }
    defer session.Close()
    return nil
}); err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

// When you're done, kill and remove the container
err = pool.Purge(resource)
```
