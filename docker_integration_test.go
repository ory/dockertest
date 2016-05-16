package dockertest

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	rethink "github.com/dancannon/gorethink"
	"github.com/garyburd/redigo/redis"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/mattbaird/elastigo/lib"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/mgo.v2"
)

func TestConnectToRethinkDB(t *testing.T) {
	container, err := Deploy(RethinkDB2)
	require.Nil(t, err)
	defer container.Destroy()

	session, err := rethink.Connect(rethink.ConnectOpts{Address: container.ServiceURL()})
	assert.Nil(t, err)
	defer session.Close()
}

func TestConnectToPostgreSQL(t *testing.T) {
	container, err := Deploy(PostgreSQL9)
	require.Nil(t, err)
	defer container.Destroy()

	db, err := sql.Open("postgres", container.ServiceURL())
	assert.Nil(t, err)
	defer db.Close()
}

func TestConnectToPostgreSQLWithCustomizedDB(t *testing.T) {
	container, err := Deploy(PostgreSQL9)
	require.Nil(t, err)
	defer container.Destroy()

	customizedDB := "db0001"
	gotURL, err := SetUpPostgreDatabase(customizedDB, container.ServiceURL())
	assert.Nil(t, err)
	assert.True(t, strings.Contains(gotURL, customizedDB),
		fmt.Sprintf("url(%s) should contains tag(%s)", gotURL, customizedDB))

	db, err := sql.Open("postgres", container.ServiceURL())
	assert.Nil(t, err)
	defer db.Close()
}

func TestConnectToRabbitMQ(t *testing.T) {
	container, err := Deploy(RabbitMQ3)
	require.Nil(t, err)
	defer container.Destroy()

	amqp, err := amqp.Dial(container.ServiceURL())
	assert.Nil(t, err)
	defer amqp.Close()
}

func TestConnectToMySQL(t *testing.T) {
	for _, spec := range []Specification{Mysql55, Mysql56, Mysql57, MariaDB55, MariaDB100, MariaDB101} {
		container, err := Deploy(spec)
		require.Nil(t, err, spec.Image)
		defer container.Destroy()

		db, err := sql.Open("mysql", container.ServiceURL())
		assert.Nil(t, err)
		defer db.Close()
	}
}

func TestConnectToMySQLWithCustomizedDB(t *testing.T) {
	for _, spec := range []Specification{Mysql55, Mysql56, Mysql57, MariaDB55, MariaDB100, MariaDB101} {
		container, err := Deploy(spec)
		require.Nil(t, err)
		defer container.Destroy()

		customizedDB := "db0001"
		gotURL, err := SetUpMySQLDatabase(customizedDB, container.ServiceURL())
		assert.Nil(t, err)
		assert.True(t, strings.Contains(gotURL, customizedDB),
			fmt.Sprintf("url(%s) should contains tag(%s)", gotURL, customizedDB))

		db, err := sql.Open("mysql", gotURL)
		assert.Nil(t, err)
		defer db.Close()
	}
}

func TestConnectToMongoDB(t *testing.T) {
	container, err := Deploy(MongoDB3)
	require.Nil(t, err)
	defer container.Destroy()

	db, err := mgo.Dial(container.ServiceURL())
	assert.Nil(t, err)
	assert.Nil(t, db.DB("test").C("test").Insert(map[string]string{"test": "test"}))
}

func TestConnectToElasticSearch(t *testing.T) {
	container, err := Deploy(ElasticSearch2)
	require.Nil(t, err)
	defer container.Destroy()

	segs := strings.Split(container.ServiceURL(), ":")
	require.Len(t, segs, 2)

	conn := elastigo.NewConn()
	conn.Domain = segs[0]
	conn.Port = segs[1]
	resp, err := conn.Health()
	defer conn.Close()
	assert.Nil(t, err)
	assert.Equal(t, "green", resp.Status)
}

func TestConnectToRedis(t *testing.T) {
	for _, spec := range []Specification{Redis3, Redis30, Redis32} {
		container, err := Deploy(spec)
		require.Nil(t, err)
		defer container.Destroy()

		client, err := redis.DialTimeout("tcp", container.ServiceURL(), 10*time.Second, 10*time.Second, 10*time.Second)
		assert.Nil(t, err)
		require.NotNil(t, client)

		reply, err := redis.String(client.Do("echo", "Hello, World!"))
		assert.Nil(t, err)
		assert.Equal(t, "Hello, World!", reply)

		defer client.Close()
	}
}

func TestConnectToNSQLookupd(t *testing.T) {
	container, err := Deploy(NSQLookupd)
	require.Nil(t, err)
	defer container.Destroy()

	resp, err := http.Get(fmt.Sprintf("%s/ping", container.URLs()["http"]))
	require.Nil(t, err)
	require.Equal(t, resp.StatusCode, 200)
}

func TestConnectToNSQd(t *testing.T) {
	container, err := Deploy(NSQd)
	require.Nil(t, err)
	defer container.Destroy()

	resp, err := http.Get(fmt.Sprintf("%s/ping", container.URLs()["http"]))
	require.Nil(t, err)
	require.Equal(t, resp.StatusCode, 200)
}

func TestCustomContainer(t *testing.T) {
	container, err := Deploy(Specification{
		Image: "ubuntu:trusty",
		ImageArguments: []string{"python3", "-c", `import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
PORT = 3000

class HelloWorld(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-type","text/plain")
        self.end_headers()
 
        self.wfile.write(bytes("Hello world!", "utf8"))
        return
httpd = HTTPServer(("", PORT), HelloWorld)
print("serving at port", PORT)
sys.stdout.flush()
httpd.serve_forever()
`},
		Waiter: RegexWaiter("serving at port 3000"),
		Services: SimpleServiceMap{
			"main": SimpleService(3000, "http://{{.}}"),
		},
	})
	require.Nil(t, err)
	defer container.Destroy()

	resp, err := http.Get(container.ServiceURL())
	require.Nil(t, err)
	require.Equal(t, resp.StatusCode, 200)
}

func TestHaveImage(t *testing.T) {
	assert := assert.New(t)

	assert.NoError(Pull("postgres"))
	assert.NoError(Pull("postgres:9.4.6"))

	tests := []struct {
		name string
		have bool
	}{
		{
			name: "postgres:latest",
			have: true,
		},
		{
			name: "postgres",
			have: true,
		},
		{
			name: "postgres:9.4.6",
			have: true,
		},
		{
			name: "postgres:9.4",
			have: false,
		},
		{
			name: "postgres1",
			have: false,
		},
		{
			name: "",
			have: false,
		},
	}

	for idx, tt := range tests {
		indexStr := fmt.Sprintf("test index: %d", idx)
		have, err := HaveImage(tt.name)
		assert.NoError(err, indexStr)
		assert.Equal(tt.have, have, indexStr)
	}
}
