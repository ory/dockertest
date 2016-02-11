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
	"github.com/mattbaird/elastigo/lib"
	. "github.com/ory-am/dockertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rethink "github.com/dancannon/gorethink"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
)

func TestConnectToRethinkDB(t *testing.T) {
	c, err := ConnectToRethinkDB(20, time.Second, func(url string) bool {
		session, err := rethink.Connect(rethink.ConnectOpts{Address: url})
		if err != nil {
			return false
		}
		defer session.Close()
		return true
	})
	assert.Nil(t, err)
	defer c.KillRemove()
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
	assert.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToMySQL(t *testing.T) {
	c, err := ConnectToMySQL(20, time.Second, func(url string) bool {
		db, err := sql.Open("mysql", url)
		if err != nil {
			return false
		}
		defer db.Close()
		return true
	})
	assert.Nil(t, err)
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
	assert.Nil(t, err)
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
	assert.Nil(t, err)
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
	assert.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToNSQLookupd(t *testing.T) {
	c, err := ConnectToNSQLookupd(15, time.Millisecond*500, func(ip string, httpPort int, tcpPort int) bool {
		resp, err := http.Get(fmt.Sprintf("http://%s:%d/ping", ip, httpPort))
		require.Nil(t, err)
		require.Equal(t, resp.StatusCode, 200)

		return true
	})
	assert.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToNSQd(t *testing.T) {
	c, err := ConnectToNSQd(15, time.Millisecond*500, func(ip string, httpPort int, tcpPort int) bool {
		resp, err := http.Get(fmt.Sprintf("http://%s:%d/ping", ip, httpPort))
		require.Nil(t, err)
		require.Equal(t, resp.StatusCode, 200)
		return true
	})
	assert.Nil(t, err)
	defer c.KillRemove()
}
