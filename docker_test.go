package dockertest

import (
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"gopkg.in/mgo.v2"
	"testing"
)

func TestMongo(t *testing.T) {
	containerID, ip, port, err := SetupMongoContainer()
	assert.Nil(t, err)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}

	assert.NotEmpty(t, containerID)
	assert.NotEmpty(t, ip)
	assert.True(t, port != 0)

	defer containerID.KillRemove()
	url := fmt.Sprintf("%s:%d", ip, port)
	sess, err := mgo.Dial(url)
	assert.Nil(t, err)
	assert.NotNil(t, sess)
	defer sess.Close()
}

func TestMySQL(t *testing.T) {
	c, ip, port, err := SetupMySQLContainer()
	assert.Nil(t, err)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer c.KillRemove()

	url := fmt.Sprintf("mysql://%s:%s@%s:%d/", MySQLUsername, MySQLPassword, ip, port)
	db, err := sql.Open("mysql", url)
	assert.Nil(t, err)
	assert.NotNil(t, db)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer db.Close()
}

func TestPostgres(t *testing.T) {
	c, ip, port, err := SetupPostgreSQLContainer()
	assert.Nil(t, err)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer c.KillRemove()

	url := fmt.Sprintf("postgres://%s:%s@%s:%d/", PostgresUsername, PostgresPassword, ip, port)
	db, err := sql.Open("postgres", url)
	assert.Nil(t, err)
	assert.NotNil(t, db)
	if err != nil {
		t.Logf("%s", err.Error())
		return
	}
	defer db.Close()
}
