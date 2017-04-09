package dockertest

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var docker = os.Getenv("DOCKER_URL")
var pool *Pool

func TestMain(m *testing.M) {
	var err error
	pool, err = NewPool(docker)
	if err != nil {
		log.Fatalf("Could not connect to docker: %s", err)
	}
	os.Exit(m.Run())
}

func TestMySQL(t *testing.T) {
	resource, err := pool.Run("mysql", "5.6", []string{"MYSQL_ROOT_PASSWORD=secret"})
	require.Nil(t, err)
	assert.NotEmpty(t, resource.GetPort("3306/tcp"))

	err = pool.Retry(func() error {
		db, err := sql.Open("mysql", fmt.Sprintf("root:secret@(localhost:%s)/mysql", resource.GetPort("3306/tcp")))
		if err != nil {
			return err
		}
		return db.Ping()
	})
	require.Nil(t, err)
	require.Nil(t, pool.Purge(resource))
}

func TestPostgres(t *testing.T) {
	resource, err := pool.Run("postgres", "9.5", nil)
	require.Nil(t, err)
	assert.NotEmpty(t, resource.GetPort("5432/tcp"))

	err = pool.Retry(func() error {
		db, err := sql.Open("postgres", fmt.Sprintf("postgres://postgres:secret@localhost:%s/postgres?sslmode=disable", resource.GetPort("5432/tcp")))
		if err != nil {
			return err
		}
		return db.Ping()
	})
	require.Nil(t, err)
	require.Nil(t, pool.Purge(resource))
}

func TestMongo(t *testing.T) {
	options := &RunOptions{
		Repository: "mongo",
		Tag:        "3.3.12",
		Cmd:        []string{"mongod", "--smallfiles"},
	}
	resource, err := pool.RunWithOptions(options)
	require.Nil(t, err)
	port := resource.GetPort("27017/tcp")
	assert.NotEmpty(t, port)

	err = pool.Retry(func() error {
		response, err := http.Get(fmt.Sprintf("http://127.0.0.1:%s", port))

		if err != nil {
			return err
		}

		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("could not connect to resource")
		}

		return nil
	})
	require.Nil(t, err)
	require.Nil(t, pool.Purge(resource))
}
