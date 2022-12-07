```go
var minioClient *minio.Client
var err error

pool, err := dockertest.NewPool("")
if err != nil {
    log.Fatalf("Could not construct pool: %s", err)
}

err = pool.Client.Ping()
if err != nil {
    log.Fatalf("Could not connect to Docker: %s", err)
}

options := &dockertest.RunOptions{
    Repository: "minio/minio",
    Tag:        "latest",
    Cmd:        []string{"server", "/data"},
    PortBindings: map[dc.Port][]dc.PortBinding{
        "9000/tcp": []dc.PortBinding{{HostPort: "9000"}},
    },
    Env: []string{"MINIO_ACCESS_KEY=MYACCESSKEY", "MINIO_SECRET_KEY=MYSECRETKEY"},
}

resource, err := pool.RunWithOptions(options)
if err != nil {
    log.Fatalf("Could not start resource: %s", err)
}

endpoint := fmt.Sprintf("localhost:%s", resource.GetPort("9000/tcp"))
// or you could use the following, because we mapped the port 9000 to the port 9000 on the host
// endpoint := "localhost:9000"

// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
// the minio client does not do service discovery for you (i.e. it does not check if connection can be established), so we have to use the health check
if err := pool.Retry(func() error {
    url := fmt.Sprintf("http://%s/minio/health/live", endpoint)
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("status code not OK")
    }
    return nil
}); err != nil {
    log.Fatalf("Could not connect to docker: %s", err)
}

// now we can instantiate minio client
minioClient, err := minio.New(endpoint, &minio.Options{
    Creds:  credentials.NewStaticV4("MYACCESSKEY", "MYSECRETKEY", ""),
    Secure: false,
})
if err != nil {
    log.Println("Failed to create minio client:", err)
    return err
}
log.Printf("%#v\n", minioClient) // minioClient is now set up

// now we can use the client, for example, to list the buckets
buckets, err := minioClient.ListBuckets(context.Background())
if err != nil {
    log.Fatalf("error while listing buckets: %v", err)
}
fmt.Printf("buckets: %+v", buckets)

// When you're done, kill and remove the container
if err = pool.Purge(resource); err != nil {
    log.Fatalf("Could not purge resource: %s", err)
}
```
