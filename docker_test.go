package dockertest_test

import (
	. "github.com/ory-am/dockertest"
	"github.com/stretchr/testify/require"
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
