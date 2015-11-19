package dockertest_test

import (
	"database/sql"
	. "github.com/ory-am/dockertest"
	"github.com/stretchr/testify/require"
	"gopkg.in/mgo.v2"
	"testing"
	"time"
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
