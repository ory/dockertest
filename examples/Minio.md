```go
var minioClient *minio.Client
var err error

pool, err = dockertest.NewPool("")
if err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

options := &dockertest.RunOptions{
    Repository: "minio/minio",
    Tag:        "latest",
    Cmd:        []string{"server", "/data"},
    PortBindings: map[dc.Port][]dc.PortBinding{
        "9000": []dc.PortBinding{{HostPort: "9000"}},
    },
    Env: []string{"MINIO_ACCESS_KEY=MYACCESSKEY", "MINIO_SECRET_KEY=MYSECRETKEY"},
}

resource, err := pool.RunWithOptions(options)
if err != nil {
    log.Fatalf("Could not start resource: %s", err)
}

endpoint := fmt.Sprintf("localhost:%s", resource.GetPort("9000/tcp"))

// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
if err := pool.Retry(func() error {
    minioClient, err := minio.New(endpoint, "MYACCESSKEY", "MYSECRETKEY", true)
    if err != nil {
        log.Println("Failed to create minio client:", err)
        return err
    }
    log.Printf("%#v\n", minioClient) // minioClient is now set up
    return nil
}); err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

// When you're done, kill and remove the container
err = pool.Purge(resource)
```
