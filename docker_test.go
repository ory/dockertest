package dockertest

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"gopkg.in/mgo.v2"
	"testing"
"log"
)

func TestMongo(t *testing.T) {
	containerID, ip, port, err := SetupMongoContainer()
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
	require.NotNil(t, sess)
	defer sess.Close()
}

func TestMySQL(t *testing.T) {
	c, ip, port, err := SetupMySQLContainer()
	require.Nil(t, err)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer c.KillRemove()

	url := fmt.Sprintf("mysql://%s:%s@%s:%d/", MySQLUsername, MySQLPassword, ip, port)
	log.Printf("Dialing mysql at %s", url)
	db, err := sql.Open("mysql", url)
	require.Nil(t, err)
	require.NotNil(t, db)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer db.Close()
}

func TestPostgres(t *testing.T) {
	c, ip, port, err := SetupPostgreSQLContainer()
	require.Nil(t, err)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer c.KillRemove()

	url := fmt.Sprintf("postgres://%s:%s@%s:%d/", PostgresUsername, PostgresPassword, ip, port)
	log.Printf("Dialing postgres at %s", url)
	db, err := sql.Open("postgres", url)
	require.Nil(t, err)
	require.NotNil(t, db)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer db.Close()
}
