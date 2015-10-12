# dockertest

[![Build Status](https://travis-ci.org/ory-am/dockertest.svg)](https://travis-ci.org/ory-am/dockertest)

A suite for testing with docker. Based on  [docker.go](https://github.com/camlistore/camlistore/blob/master/pkg/test/dockertest/docker.go) from [camlistore](https://github.com/camlistore/camlistore).
This fork detects automatically, if [docker-machine](https://docs.docker.com/machine/) is installed. If it is, you are able to use the docker integration on Windows and Mac OSX as well without any additional work.

To avoid port collisions when using docker-machine, dockertest chooses a random port to bind the requested image to.

## Examples

### Mongo Container

```go
import "github.com/ory-am/dockertest"
import "gopkg.in/mgo.v2"

func Foobar() {
  // Start MongoDB Docker container
  containerID, ip, port, err := dockertest.SetupMongoContainer()

  if err != nil {
    return err
  }

  // kill the container on deference
  defer containerID.KillRemove()

  url := fmt.Sprintf("%s:%d", ip, port)
  sess, err := mgo.Dial(url)
  if err != nil {
    return err
  }

  defer sess.Close()
  // ...
}
```

### MySQL Container

```go
import "github.com/ory-am/dockertest"
import "github.com/go-sql-driver/mysql"
import "database/sql"

func Foobar() {
    c, ip, port, err := dockertest.SetupMySQLContainer()
    if err != nil {
        return
    }
    defer c.KillRemove()

    url := fmt.Sprintf("mysql://%s:%s@%s:%d/", dockertest.MySQLUsername, dockertest.MySQLPassword, ip, port)
    db, err := sql.Open("mysql", url)
    if err != nil {
        return
    }

    defer db.Close()
    // ...
}
```
### Postgres Container

```go
import "github.com/ory-am/dockertest"
import "github.com/lib/pq"
import "database/sql"

func Foobar() {
    c, ip, port, err := dockertest.SetupPostgresContainer()
    if err != nil {
        return
    }
    defer c.KillRemove()

    url := fmt.Sprintf("postgres://%s:%s@%s:%d/", dockertest.PostgresUsername, dockertest.PostgresPassword, ip, port)
    db, err := sql.Open("postgres", url)
    if err != nil {
        return
    }

    defer db.Close()
    // ...
}
```

Thanks to our sponsors: Ory GmbH & Imarum GmbH
