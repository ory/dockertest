package dockertest_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"gopkg.in/mgo.v2"

	"github.com/mattbaird/elastigo/lib"
	. "github.com/ory-am/dockertest"
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
		defer conn.Close()
		return true
	})
	require.Nil(t, err)
	defer c.KillRemove()
}
