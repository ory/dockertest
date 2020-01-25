package dockertest

import (
	"bytes"
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
	dc "github.com/ory/dockertest/v3/docker"
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

func TestPostgres(t *testing.T) {
	resource, err := pool.Run("postgres", "9.5", nil)
	require.Nil(t, err)
	assert.NotEmpty(t, resource.GetPort("5432/tcp"))

	assert.NotEmpty(t, resource.GetBoundIP("5432/tcp"))

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
		Cmd:        []string{"mongod", "--smallfiles", "--port", "3000"},
		// expose a different port
		ExposedPorts: []string{"3000"},
	}
	resource, err := pool.RunWithOptions(options)
	require.Nil(t, err)
	port := resource.GetPort("3000/tcp")
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

func TestContainerWithName(t *testing.T) {
	resource, err := pool.RunWithOptions(
		&RunOptions{
			Name:       "db",
			Repository: "postgres",
			Tag:        "9.5",
		})
	require.Nil(t, err)
	assert.Equal(t, "/db", resource.Container.Name)

	require.Nil(t, pool.Purge(resource))
}

func TestContainerWithLabels(t *testing.T) {
	labels := map[string]string{
		"my": "label",
	}
	resource, err := pool.RunWithOptions(
		&RunOptions{
			Name:       "db",
			Repository: "postgres",
			Tag:        "9.5",
			Labels:     labels,
		})
	require.Nil(t, err)
	assert.EqualValues(t, labels, resource.Container.Config.Labels, "labels don't match")

	require.Nil(t, pool.Purge(resource))
}

func TestContainerWithPortBinding(t *testing.T) {
	resource, err := pool.RunWithOptions(
		&RunOptions{
			Repository: "postgres",
			Tag:        "9.5",
			PortBindings: map[dc.Port][]dc.PortBinding{
				"5432/tcp": {{HostIP: "", HostPort: "5433"}},
			},
		})
	require.Nil(t, err)
	assert.Equal(t, "5433", resource.GetPort("5432/tcp"))

	require.Nil(t, pool.Purge(resource))
}

func TestBuildImage(t *testing.T) {
	// Create Dockerfile in temp dir
	dir, _ := ioutil.TempDir("", "dockertest")
	defer os.RemoveAll(dir)

	dockerfilePath := dir + "/Dockerfile"
	ioutil.WriteFile(dockerfilePath,
		[]byte("FROM postgres:9.5"),
		0644,
	)

	resource, err := pool.BuildAndRun("postgres-test", dockerfilePath, nil)
	require.Nil(t, err)

	assert.Equal(t, "/postgres-test", resource.Container.Name)
	require.Nil(t, pool.Purge(resource))
}

func TestExpire(t *testing.T) {
	resource, err := pool.Run("postgres", "9.5", nil)
	require.Nil(t, err)
	assert.NotEmpty(t, resource.GetPort("5432/tcp"))

	assert.NotEmpty(t, resource.GetBoundIP("5432/tcp"))

	err = pool.Retry(func() error {
		db, err := sql.Open("postgres", fmt.Sprintf("postgres://postgres:secret@localhost:%s/postgres?sslmode=disable", resource.GetPort("5432/tcp")))
		if err != nil {
			return err
		}
		err = db.Ping()
		if err != nil {
			return nil
		}
		err = resource.Expire(1)
		require.Nil(t, err)
		time.Sleep(5 * time.Second)
		err = db.Ping()
		require.NotNil(t, err)
		return nil
	})
	require.Nil(t, err)

	require.Nil(t, pool.Purge(resource))
}

func TestContainerWithShMzSize(t *testing.T) {
	shmemsize := int64(1024 * 1024)
	resource, err := pool.RunWithOptions(
		&RunOptions{
			Name:       "db",
			Repository: "postgres",
			Tag:        "9.5",
		}, func(hostConfig *dc.HostConfig) {
			hostConfig.ShmSize = shmemsize
		})
	require.Nil(t, err)
	assert.EqualValues(t, shmemsize, resource.Container.HostConfig.ShmSize, "shmsize don't match")

	require.Nil(t, pool.Purge(resource))
}

func TestContainerByName(t *testing.T) {
	got, err := pool.RunWithOptions(
		&RunOptions{
			Name:       "db",
			Repository: "postgres",
			Tag:        "9.5",
		})
	require.Nil(t, err)

	want, ok := pool.ContainerByName("db")
	require.True(t, ok)

	require.Equal(t, got, want)

	require.Nil(t, pool.Purge(got))
}

func TestRemoveContainerByName(t *testing.T) {
	_, err := pool.RunWithOptions(
		&RunOptions{
			Name:       "db",
			Repository: "postgres",
			Tag:        "9.5",
		})
	require.Nil(t, err)

	err = pool.RemoveContainerByName("db")
	require.Nil(t, err)

	resource, err := pool.RunWithOptions(
		&RunOptions{
			Name:       "db",
			Repository: "postgres",
			Tag:        "9.5",
		})
	require.Nil(t, err)
	require.Nil(t, pool.Purge(resource))
}

func TestExec(t *testing.T) {
	resource, err := pool.Run("postgres", "9.5", nil)
	require.Nil(t, err)
	assert.NotEmpty(t, resource.GetPort("5432/tcp"))
	assert.NotEmpty(t, resource.GetBoundIP("5432/tcp"))

	defer resource.Close()

	var pgVersion string
	err = pool.Retry(func() error {
		db, err := sql.Open("postgres", fmt.Sprintf("postgres://postgres:secret@localhost:%s/postgres?sslmode=disable", resource.GetPort("5432/tcp")))
		if err != nil {
			return err
		}
		return db.QueryRow("SHOW server_version").Scan(&pgVersion)
	})
	require.Nil(t, err)

	var stdout bytes.Buffer
	exitCode, err := resource.Exec(
		[]string{"psql", "-qtAX", "-U", "postgres", "-c", "SHOW server_version"},
		ExecOptions{StdOut: &stdout},
	)
	require.Nil(t, err)
	require.Zero(t, exitCode)

	require.Equal(t, pgVersion, strings.TrimRight(stdout.String(), "\n"))
}
