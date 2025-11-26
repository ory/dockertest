# Creating Multiple Test Containers

## 🛠️ Introduction

Testing in isolated environments is crucial to ensuring the reliability and
consistency of applications. However, creating isolated test environments can be
challenging when dealing with Go services that interact with multiple
dependencies. The library [ory/dockertest](https://github.com/ory/dockertest)
simplifies the process by enabling developers to spin up Docker containers to
test their Go applications.

This guide illustrates creating multiple test containers for a straightforward
REST API in Go, which relies on a PostgreSQL database. We will focus on writing
API tests that involve setting up API and database test containers to execute
comprehensive end-to-end tests.

---

## 📋 Prerequisites

Before we start, ensure you have the following installed:

- Docker/Podman
- Go
- Git

---

## 📂 Project Overview

Clone the project to get started:

```sh
git clone https://github.com/akoserwal/multi-containers-dockertest
```

Project structure:

```
.
├── Dockerfile
├── LocalTestContainers.go
├── Makefile
├── README.md
├── db
│   └── migrations
│       ├── 000001_create_items_table.down.sql
│       └── 000001_create_items_table.up.sql
├── go.mod
├── go.sum
├── main.go
├── main_test.go
├── secrets
│   ├── db.name
│   ├── db.password
│   └── db.user
└── start-postgresql.sh
```

**File descriptions:**

- `Dockerfile`: Builds container images of the sample Items API.
- `LocalTestContainers.go`: Contains logic for creating test containers for the
  API and PostgreSQL.
- `Makefile`: Includes commands to run the application and tests.
- `db`: Holds database migrations and SQL files.
- `main.go`: Exposes the Items REST API on port 8000.
- `main_test.go`: Creates multiple test containers and runs end-to-end tests.
- `secrets`: Stores database credentials.
- `start-postgresql.sh`: Script to create a PostgreSQL container.

---

## 🚀 Running the Application

Create the PostgreSQL database container:

```sh
make postgres_up
```

Verify the container is running:

```sh
docker ps
```

Run database migrations:

```sh
make migrate_up
```

Run the REST API:

```sh
make run
```

Test the API with `curl`:

```sh
curl -H 'Content-Type: application/json' \
  -d '{ "id":1,"name":"asdad","price":345 }' \
  -X POST \
  http://localhost:8000/items
```

Teardown the application:

```sh
make postgres_down
```

---

## ✅ Running the Tests

Run end-to-end tests:

```sh
make test
```

Example output:

```sh
=== RUN   TestCreateItem
--- PASS: TestCreateItem (0.03s)
=== RUN   TestGetItem
--- PASS: TestGetItem (0.00s)
=== RUN   TestUpdateItem
--- PASS: TestUpdateItem (0.00s)
=== RUN   TestDeleteItem
--- PASS: TestDeleteItem (0.00s)
PASS
```

---

## 🏗️ Creating Multiple Test Containers

Let's dive into how to create multiple test containers. In the main_test.go Use
TestMain, which provides the functionality to control the lifecycle of the
tests. In the TestMainwill initialise the CreateLocalTestContainer(), Which sets
up multiple test containers for the REST API, migration and Postgres database.
m.Run() will run all the tests within the package.

Use `TestMain` to control the test lifecycle:

```go
func TestMain(m *testing.M) {
    var err error
    localTestContainer, err = CreateLocalTestContainer()
    if err != nil {
        fmt.Printf("Error initializing Docker localTestContainer: %s", err)
        os.Exit(1)
    }

    var wg sync.WaitGroup
    wg.Add(1)
    go func(p string) {
        err := waitForServiceToBeReady(p)
        if err != nil {
            localTestContainer.Close()
            panic(fmt.Errorf("Error waiting for local container to start: %w", err))
        }
        wg.Done()
    }(localTestContainer.appport)

    wg.Wait()
    result := m.Run()
    localTestContainer.Close()
    os.Exit(result)
}
```

Let's take a closer look at the process. The code uses "ory/dockertest" to
create a new pool and configure the Docker running environment. First, it checks
if a network with a specific name(`app-datastore`) exists, and if not, it
creates one call createNetwork. Then, it sets up Postgres, Migration, and
Application test containers. All three containers can connect to the same
network.

Create the test containers:

```go
func CreateLocalTestContainer() (*LocalTestContainer, error) {
    pool, err := dockertest.NewPool("")
    if err != nil {
        log.Fatalf("Could not construct pool: %s", err)
        return nil, err
    }

    networkName := "app-datastore"
    network, err := createNetwork(networkName, err, pool)

    dbresource := createPostgresDB(err, pool, network)
    log.Printf("Postgresql db container: %s", dbresource.Container.Name)

    port := "5432"
    name := strings.Trim(dbresource.Container.Name, "/")
    databaseUrl := fmt.Sprintf("postgres://user_name:secret@%s:%s/dbname?sslmode=disable", name, port)
    log.Println("Connecting to database on url: ", databaseUrl)

    testDBConnectivity(pool, dbresource)

    tempDir, err := os.MkdirTemp("", "migrations")
    if err != nil {
        log.Fatalf("Could not create temp dir: %s", err)
    }
    defer os.RemoveAll(tempDir)

    copyDir("./db/migrations", tempDir)
    dbmigrate := createMigration(err, pool, network, databaseUrl, tempDir, dbresource)
    log.Printf("Migration container: %s", dbmigrate.Container.Name)

    appresource := createAppContainer(err, pool, databaseUrl, network)
    appport := appresource.GetPort("8000/tcp")

    log.Printf("Items API container %s", appresource.Container.Name)

    return &LocalTestContainer{
        appName:            appresource.Container.Name,
        dbName:             dbresource.Container.Name,
        appcontainer:       appresource,
        dbcontainer:        dbresource,
        dbmigratecontainer: dbmigrate,
        appport:            appport,
        pool:               pool,
        network:            network.ID,
    }, nil
}
```

Next, it will callcreatePostgreDB to create a Postgres database test container.
Postgres with the latest image will be pulled from the container registry, and a
Postgres database container will be created with configured environment
variables.

```go
 func createPostgresDB(err error, pool *dockertest.Pool, network *docker.Network) *dockertest.Resource {
 // pulls an image, creates a container based on it and runs it
 dbresource, err := pool.RunWithOptions(&dockertest.RunOptions{
  Repository: "postgres",
  Tag:        "latest",
  Env: []string{
   "POSTGRES_PASSWORD=secret",
   "POSTGRES_USER=user_name",
   "POSTGRES_DB=dbname",
   "listen_addresses = '*'",
  },
  NetworkID: network.ID,
 }, func(config *docker.HostConfig) {
  // set AutoRemove to true so that stopped container goes away by itself
  config.AutoRemove = true
  config.RestartPolicy = docker.RestartPolicy{Name: "no"}
 })
 if err != nil {
  log.Fatalf("Could not start dbresource: %s", err)
 }
 return dbresource
}
```

Next, Run the createMigration to create a Migration container. This container
will mount the files from the temporary directory and run the migration command
by making a connection to the Postgres database created in the previous step.

```go
 func createMigration(err error, pool *dockertest.Pool, network *docker.Network, databaseUrl string, tempDir string, dbresource *dockertest.Resource) *dockertest.Resource {
 dbmigrate, err := pool.RunWithOptions(&dockertest.RunOptions{
  Repository: "migrate/migrate",
  Tag:        "latest",
  NetworkID:  network.ID,
  Cmd: []string{"-path", "/migrations",
   "-database", databaseUrl,
   "-verbose", "up", "2"},
  Mounts: []string{
   fmt.Sprintf("%s:/migrations", tempDir),
  },
 }, func(config *docker.HostConfig) {
  config.NetworkMode = "bridge"
  config.AutoRemove = true
  config.RestartPolicy = docker.RestartPolicy{Name: "no"}
 })
 if err != nil {
  log.Fatalf("Could not start dbresource: %s", err)
 }
 // Wait for the migration to complete
 if err := pool.Retry(func() error {
  _, err := dbmigrate.Exec([]string{"migrate", "-path", "/migrations", "-database", fmt.Sprintf("postgres://user_name:secret@localhost:%s/dbname?sslmode=disable", dbresource.GetPort("5432/tcp")), "up", "2"}, dockertest.ExecOptions{})
  return err
 }); err != nil {
  log.Fatalf("Migration failed: %s", err)
 }
 return dbmigrate
}
```

Finally, createAppContainerwill create the REST API container using Dockerfile
the present in the same directory. Configuring the Platform `Linux/amd64` and
passing the build argument `amd64` as the TARGETARCH will ensure that the test
runs in the GitHub workflow.

```go
 func createAppContainer(err error, pool *dockertest.Pool, databaseUrl string, network *docker.Network) *dockertest.Resource {
 targetArch := "amd64" // or "arm64", depending on your needs
 appresource, err := pool.BuildAndRunWithBuildOptions(&dockertest.BuildOptions{
  Dockerfile: "Dockerfile", // Path to your Dockerfile
  ContextDir: ".",          // Context directory for the Dockerfile
  Platform:   "linux/amd64",
  BuildArgs: []docker.BuildArg{
   {Name: "TARGETARCH", Value: targetArch},
  },
 }, &dockertest.RunOptions{
  Name:      "app",
  Env:       []string{fmt.Sprintf("DB_CONN_URL=%s", databaseUrl)},
  NetworkID: network.ID,
 }, func(config *docker.HostConfig) {
  config.NetworkMode = "bridge"
  config.AutoRemove = true
  config.RestartPolicy = docker.RestartPolicy{Name: "no"}
 })
 pool.MaxWait = 3 * time.Minute
 return appresource
}
```

Once all three containers are up and running. Tests main_test.go such as
TestCreateItem, TestGetItem, TestUpdateItem, and TestDeleteItem will begin
running.

We can use it to configure the request URL and get the running container port.

```go
 func TestCreateItem(t *testing.T) {
 // Create an item to test retrieval
 newItem := Item{
  Name:  "Testitem",
  Price: 201,
 }
 url := fmt.Sprintf("http://localhost:%s/%s", localTestContainer.appport, "items")
 jsonValue, _ := json.Marshal(newItem)
 createReq, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonValue))
 createReq.Header.Set("Content-Type", "application/json")

 client := http.DefaultClient
 createResp, err := client.Do(createReq)
 if err != nil {
  t.Fatalf("Failed to send request: %v", err)
 }
 defer createResp.Body.Close()

 var createdItem Item
 json.NewDecoder(createResp.Body).Decode(&createdItem)

 // Test retrieving the created item
 getReq, _ := http.NewRequest("GET", fmt.Sprintf("%s/%d", url, createdItem.ID), nil)
 getResp, err := client.Do(getReq)
 if err != nil {
  t.Fatalf("Failed to send request: %v", err)
 }
 defer getResp.Body.Close()

 assert.Equal(t, http.StatusOK, getResp.StatusCode)

 var fetchedItem Item
 json.NewDecoder(getResp.Body).Decode(&fetchedItem)

 assert.Equal(t, createdItem.Name, fetchedItem.Name)
 assert.Equal(t, createdItem.Price, fetchedItem.Price)
}
```

Once all tests run, TestMain calls localTestContainer.Close to clean up running
docker test containers.

## 🏁 Conclusion

Using `ory/dockertest`, we can create isolated test environments with multiple
containers for API, database, and migrations. This approach makes it easier to
write reliable end-to-end tests for Go applications interacting with various
services.

Want to enhance this further? Let me know! 🚀

---

_🔗 Check out the project repository:
[multi-containers-dockertest](https://github.com/akoserwal/multi-containers-dockertest)_

Follow the guide:
[Creating Multiple Test Containers with ory/dockertest in Go](https://akoserwal.medium.com/creating-multiple-test-containers-with-ory-dockertest-in-go-5b8311614e7b)

Code:
[LocalTestContainers](https://github.com/akoserwal/multi-containers-dockertest/blob/main/LocalTestContainers.go)

Author: Abhishek Koserwal [https://github.com/akoserwal]
