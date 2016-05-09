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

func TestConnectToPostgreSQLWithCustomizedDB(t *testing.T) {
	c, err := ConnectToPostgreSQL(15, time.Millisecond*500, func(url string) bool {
		customizedDB := "db0001"
		gotURL, err := SetUpPostgreDatabase(customizedDB, url)
		if err != nil {
			return false
		}
		assert.True(t, strings.Contains(gotURL, customizedDB),
			fmt.Sprintf("url(%s) should contains tag(%s)", gotURL, customizedDB))
		db, err := sql.Open("postgres", gotURL)
		if err != nil {
			return false
		}
		defer db.Close()
		return true
	})
	assert.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToRabbitMQ(t *testing.T) {
	c, err := ConnectToRabbitMQ(15, time.Millisecond*500, func(url string) bool {
		amqp, err := amqp.Dial(fmt.Sprintf("amqp://%v", url))
		if err != nil {
			return false
		}
		defer amqp.Close()
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

func TestConnectToMySQLWithCustomizedDB(t *testing.T) {
	customizedDB := "db0001"
	c, err := ConnectToMySQL(20, time.Second, func(url string) bool {
		gotURL, err := SetUpMySQLDatabase(customizedDB, url)
		if err != nil {
			return false
		}
		assert.True(t, strings.Contains(gotURL, customizedDB),
			fmt.Sprintf("url(%s) should contains tag(%s)", gotURL, customizedDB))
		db, err := sql.Open("mysql", gotURL)
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

func TestCustomContainer(t *testing.T) {
	c1, ip, port, err := SetupCustomContainer("rabbitmq", 5672, 10*time.Second)
	assert.Nil(t, err)
	defer c1.KillRemove()

	err = ConnectToCustomContainer(fmt.Sprintf("%v:%v", ip, port), 15, time.Millisecond*500, func(url string) bool {
		amqp, err := amqp.Dial(fmt.Sprintf("amqp://%v", url))
		if err != nil {
			return false
		}
		defer amqp.Close()
		return true
	})
	assert.Nil(t, err)
}

func TestConnectToMockServer(t *testing.T) {
	c, err := ConnectToMockserver(15, time.Millisecond*500,
		func(url string) bool {
			req, err := http.NewRequest("PUT", fmt.Sprintf("%v/reset", url), nil)
			if err != nil {
				return false
			}
			_, err = http.DefaultClient.Do(req)
			return err == nil
		},
		func(url string) bool {
			req, err := http.NewRequest("PUT", fmt.Sprintf("%v/reset", url), nil)
			if err != nil {
				return false
			}
			_, err = http.DefaultClient.Do(req)
			return err == nil
		})
	assert.Nil(t, err)
	defer c.KillRemove()
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
