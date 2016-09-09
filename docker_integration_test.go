package dockertest

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"gopkg.in/mgo.v2"

	etcd "github.com/coreos/etcd/clientv3"
	rethink "github.com/dancannon/gorethink"
	"github.com/garyburd/redigo/redis"
	_ "github.com/go-sql-driver/mysql"
	"github.com/go-stomp/stomp"
	"github.com/gocql/gocql"
	consulapi "github.com/hashicorp/consul/api"
	_ "github.com/lib/pq"
	elastigo "github.com/mattbaird/elastigo/lib"
	"github.com/samuel/go-zookeeper/zk"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestConnectToActiveMQ(t *testing.T) {
	c, err := ConnectToActiveMQ(15, time.Millisecond*500, func(url string) bool {
		conn, err := stomp.Dial("tcp", url, stomp.ConnOpt.Login("admin", "admin"))
		if err != nil {
			return false
		}
		defer conn.Disconnect()
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

func TestConnectToConsul(t *testing.T) {
	BindDockerToLocalhost = "true"
	c, err := ConnectToConsul(30, time.Millisecond*500, func(address string) bool {
		config := consulapi.DefaultConfig()
		config.Address = address
		config.Token = ConsulACLMasterToken
		client, err := consulapi.NewClient(config)
		if err != nil {
			return false
		}

		_, err = client.KV().Put(&consulapi.KVPair{
			Key:   "setuptest",
			Value: []byte("setuptest"),
		}, nil)
		if err != nil {
			return false
		}

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

func TestConnectToZooKeeper(t *testing.T) {
	c, err := ConnectToZooKeeper(15, time.Millisecond*500, func(url string) bool {
		conn, _, err := zk.Connect([]string{url}, time.Second)
		if err != nil {
			return false
		}
		defer conn.Close()

		// Verify that we can perform operations, and that it's
		// a clean slate (/zookeeper should be the only path)
		children, _, err := conn.Children("/")
		if err != nil {
			return false
		}
		if len(children) != 1 {
			return false
		}
		if children[0] != "zookeeper" {
			return false
		}

		return true
	})
	assert.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToCassandra(t *testing.T) {
	// Cassandra seems to have issues if this is not set.
	// See: http://stackoverflow.com/questions/34645846/cannot-connect-to-cassandra-docker-with-cqlsh
	BindDockerToLocalhost = "true"

	env := []string{
		"-e", "CASSANDRA_RACK=TEST_RACK",
		"-e", "CASSANDRA_ENDPOINT_SNITCH=GossipingPropertyFileSnitch",
	}

	// Cassandra takes a while to start up, so we have a longer retry window
	c, err := ConnectToCassandra("3.7", 20, time.Second*5, func(url string) bool {
		cluster := gocql.NewCluster(url)
		cluster.Keyspace = "system"
		cluster.ProtoVersion = 4 // Required for cassandra 3.x

		session, err := cluster.CreateSession()
		if err != nil {
			return false
		}
		defer session.Close()

		// Verify that the environment property was applied correctly,
		// and that querying the node works.
		it := session.Query("select rack from local").Iter()
		defer it.Close()

		var rackName string
		for it.Scan(&rackName) {
			if rackName != "TEST_RACK" {
				return false
			}
		}

		return true
	}, env...)
	assert.Nil(t, err)
	defer c.KillRemove()
}

func TestConnectToEtcd(t *testing.T) {
	c, err := ConnectToEtcd(20, time.Second*10, func(address string) bool {
		client, err := etcd.New(etcd.Config{
			Endpoints:   []string{address},
			DialTimeout: 10 * time.Second,
		})
		assert.NotNil(t, client)
		return err == nil
	})
	assert.Nil(t, err)
	defer c.KillRemove()
}

func TestStartStopContainer(t *testing.T) {
	var hosts []string
	c, err := ConnectToZooKeeper(15, time.Millisecond*500, func(url string) bool {
		conn, _, err := zk.Connect([]string{url}, time.Second)
		if err != nil {
			return false
		}
		defer conn.Close()
		hosts = []string{url}

		return true
	})
	assert.NoError(t, err)
	defer c.KillRemove()

	conn, _, err := zk.Connect(hosts, time.Second)
	assert.NoError(t, err)

	testPath := "/test"
	testData := []byte("hello")

	path, err := conn.Create(testPath, testData, 0, zk.WorldACL(zk.PermAll))
	assert.NoError(t, err)
	assert.Equal(t, testPath, path)

	data, _, err := conn.Get(testPath)
	assert.NoError(t, err)
	assert.Equal(t, testData, data)

	// Let's stop the container.
	assert.NoError(t, c.Stop())

	_, _, err = conn.Get(testPath)
	assert.EqualError(t, err, zk.ErrNoServer.Error())

	assert.NoError(t, c.Start())
	data, _, err = conn.Get(testPath)
	assert.Equal(t, testData, data)
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
