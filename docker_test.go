package dockertest

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"gopkg.in/mgo.v2"
	"log"
	"testing"
	"time"
)

func TestMongo(t *testing.T) {
	containerID, ip, port, err := SetupMongoContainer(time.Second)
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
	require.Nil(t, sess.Ping())
	require.NotNil(t, sess)
	defer sess.Close()
}

func TestMySQL(t *testing.T) {
	c, ip, port, err := SetupMySQLContainer(time.Second * 10)
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
	require.Nil(t, db.Ping())
	require.NotNil(t, db)
	defer db.Close()
}

func TestPostgres(t *testing.T) {
	c, ip, port, err := SetupPostgreSQLContainer(time.Second * 10)
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
	require.Nil(t, db.Ping())
	require.NotNil(t, db)
	defer db.Close()
}
