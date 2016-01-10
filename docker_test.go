package dockertest_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"gopkg.in/mgo.v2"

	"github.com/garyburd/redigo/redis"
	. "github.com/ninnemana/dockertest"
	"github.com/ory-am/elastigo/lib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenPostgreSQLContainerConnection(t *testing.T) {
	c, db, err := OpenPostgreSQLContainerConnection(15, time.Millisecond*500)
	require.Nil(t, err)
	defer c.KillRemove()
	require.Nil(t, db.Ping())
	require.NotNil(t, db)
	defer db.Close()
}

func TestOpenMySQLContainerConnection(t *testing.T) {
	c, db, err := OpenMySQLContainerConnection(15, time.Millisecond*500)
	require.Nil(t, err)
	defer c.KillRemove()
	require.Nil(t, db.Ping())
	require.NotNil(t, db)
	defer db.Close()
}

func TestOpenMongoDBContainerConnection(t *testing.T) {
	c, db, err := OpenMongoDBContainerConnection(15, time.Millisecond*500)
	require.Nil(t, err)
	defer c.KillRemove()
	require.NotNil(t, db)
	_, err = db.DatabaseNames()
	require.Nil(t, err)
	defer db.Close()
}

func TestOpenElasticSearchContainerConnection(t *testing.T) {
	c, conn, err := OpenElasticSearchContainerConnection(15, time.Millisecond*500)
	require.Nil(t, err)
	defer c.KillRemove()
	require.NotNil(t, conn)
	_, err = conn.Health("")
	require.Nil(t, err)
	defer conn.Close()
}

func TestOpenRedisContainerConnection(t *testing.T) {
	c, client, err := OpenRedisContainerConnection(15, time.Millisecond*500)
	require.Nil(t, err)
	defer c.KillRemove()
	require.NotNil(t, client)

	v, err := client.Cmd("echo", "Hello, World!").Str()
	require.Nil(t, err)
	assert.Equal(t, "Hello, World!", v)

	defer client.Close()
}

func TestOpenNSQLookupdContainerConnection(t *testing.T) {

	c, ip, tcpPort, httpPort, err := OpenNSQLookupdContainerConnection(15, time.Millisecond*500)
	require.Nil(t, err)
	defer c.KillRemove()
	require.NotEmpty(t, ip)
	require.NotZero(t, tcpPort)
	require.NotZero(t, httpPort)

	resp, err := http.Get(fmt.Sprintf("http://%s:%d/ping", ip, httpPort))
	require.Nil(t, err)
	require.Equal(t, resp.StatusCode, 200)

}

func TestOpenNSQContainerConnection(t *testing.T) {

	c, ip, tcpPort, httpPort, err := OpenNSQdContainerConnection(15, time.Millisecond*500)
	require.Nil(t, err)
	defer c.KillRemove()
	require.NotEmpty(t, ip)
	require.NotZero(t, tcpPort)
	require.NotZero(t, httpPort)

	resp, err := http.Get(fmt.Sprintf("http://%s:%d/ping", ip, httpPort))
	require.Nil(t, err)
	require.Equal(t, resp.StatusCode, 200)

}

func TestConnectToPostgreSQL(t *testing.T) {
	c, err := ConnectToPostgreSQL(15, time.Millisecond*500, func(url string) bool {
		db, err := sql.Open("postgres", url)
		if err != nil {
			return false
		}
		defer db.Close()
		return true
	})
	require.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToMySQL(t *testing.T) {
	c, err := ConnectToMySQL(15, time.Millisecond*500, func(url string) bool {
		db, err := sql.Open("mysql", url)
		if err != nil {
			return false
		}
		defer db.Close()
		return true
	})
	require.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToMongoDB(t *testing.T) {
	c, err := ConnectToMongoDB(15, time.Millisecond*500, func(url string) bool {
		db, err := mgo.Dial(url)
		if err != nil {
			return false
		}
		defer db.Close()
		return true
	})
	require.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToElasticSearch(t *testing.T) {
	c, err := ConnectToElasticSearch(15, time.Millisecond*500, func(url string) bool {
		segs := strings.Split(url, ":")
		if len(segs) != 2 {
			return false
		}

		conn := elastigo.NewConn()
		conn.Domain = segs[0]
		conn.Port = segs[1]
		resp, err := conn.Health()
		if err != nil {
			return false
		}
		if resp.Status != "green" {
			return false
		}
		// defer conn.Close()
		return true
	})
	require.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToRedis(t *testing.T) {
	c, err := ConnectToRedis(15, time.Millisecond*500, func(url string) bool {
		client, err := redis.DialTimeout("tcp", url, 10*time.Second, 10*time.Second, 10*time.Second)
		require.Nil(t, err)
		require.NotNil(t, client)

		reply, err := redis.String(client.Do("echo", "Hello, World!"))

		require.Nil(t, err)
		assert.Equal(t, "Hello, World!", reply)

		defer client.Close()
		return true
	})
	require.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToNSQLookupd(t *testing.T) {
	c, err := ConnectToNSQLookupd(15, time.Millisecond*500, func(ip string, httpPort int, tcpPort int) bool {
		resp, err := http.Get(fmt.Sprintf("http://%s:%d/ping", ip, httpPort))
		require.Nil(t, err)
		require.Equal(t, resp.StatusCode, 200)

		return true
	})
	require.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToNSQd(t *testing.T) {
	c, err := ConnectToNSQd(15, time.Millisecond*500, func(ip string, httpPort int, tcpPort int) bool {
		resp, err := http.Get(fmt.Sprintf("http://%s:%d/ping", ip, httpPort))
		require.Nil(t, err)
		require.Equal(t, resp.StatusCode, 200)

		return true
	})
	require.Nil(t, err)
	defer c.KillRemove()
}
