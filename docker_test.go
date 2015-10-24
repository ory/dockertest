package dockertest_test

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	. "github.com/ory-am/dockertest"
	"github.com/stretchr/testify/require"
	"gopkg.in/mgo.v2"
	"log"
	"testing"
	"time"
)

var (
	Wait10s = time.Second * 10
	Wait5s  = time.Second * 5
	Wait3s  = time.Second * 3
	Wait1s  = time.Second
)

func TestMongo(t *testing.T) {
	containerID, ip, port, err := SetupMongoContainer(Wait1s)
	require.Nil(t, err)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}

	require.NotEmpty(t, containerID)
	require.NotEmpty(t, ip)
	require.True(t, port != 0)

	defer containerID.KillRemove()
	url := fmt.Sprintf("%s:%d", ip, port)
	log.Printf("Dialing mongodb at %s", url)
	sess, err := mgo.Dial(url)
	require.Nil(t, err)
	time.Sleep(Wait1s)
	require.Nil(t, sess.Ping())
	require.NotNil(t, sess)
	defer sess.Close()
}

func TestMySQL(t *testing.T) {
	c, ip, port, err := SetupMySQLContainer(Wait10s)
	require.Nil(t, err)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer c.KillRemove()

	url := fmt.Sprintf("%s:%s@tcp(%s:%d)/mysql", MySQLUsername, MySQLPassword, ip, port)
	log.Printf("Dialing mysql at %s", url)
	db, err := sql.Open("mysql", url)
	require.Nil(t, err)
	time.Sleep(Wait1s)
	require.Nil(t, db.Ping())
	require.NotNil(t, db)
	defer db.Close()
}

func TestPostgres(t *testing.T) {
	c, ip, port, err := SetupPostgreSQLContainer(Wait10s)
	require.Nil(t, err)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer c.KillRemove()

	url := fmt.Sprintf("postgres://%s:%s@%s:%d/postgres?sslmode=disable", PostgresUsername, PostgresPassword, ip, port)
	log.Printf("Dialing postgres at %s", url)
	db, err := sql.Open("postgres", url)
	require.Nil(t, err)
	time.Sleep(Wait1s)
	require.Nil(t, db.Ping())
	require.NotNil(t, db)
	defer db.Close()
}

func TestOpenPostgreSQLContainerConnection(t *testing.T) {
	db := OpenPostgreSQLContainerConnection(Wait10s, Wait1s)
	require.NotNil(t, db)
	defer db.Close()
}

func TestOpenMySQLContainerConnection(t *testing.T) {
	db := OpenMySQLContainerConnection(Wait10s, Wait1s)
	require.NotNil(t, db)
	defer db.Close()
}

func TestOpenMongoDBContainerConnection(t *testing.T) {
	db := OpenMongoDBContainerConnection(Wait1s, Wait1s)
	require.NotNil(t, db)
	defer db.Close()
}
