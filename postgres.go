package dockertest

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/lib/pq"
)

var (
	// PostgresUsername must be passed as username when connecting to postgres
	PostgresUsername = "postgres"

	// PostgresPassword must be passed as password when connecting to postgres
	PostgresPassword = "docker"
)

var PostgreSQL9 = Specification{
	Image: "postgres:9",
	Waiter: RegexWaiter(
		"PostgreSQL init process complete; ready for start up",
		"database system is ready to accept connections",
	),
	Services: SimpleServiceMap{
		"main": SimpleService(5432, fmt.Sprintf("postgres://%s:%s@{{.}}/postgres?sslmode=disable", PostgresUsername, PostgresPassword)),
	},
	Env: Env{
		"POSTGRES_PASSWORD": PostgresPassword,
	},
}

// SetUpPostgreDatabase connects postgre container with given $connectURL and also creates a new database named $databaseName
// A modified url used to connect the created database will be returned
func SetUpPostgreDatabase(databaseName, connectURL string) (modifiedURL string, err error) {
	db, err := sql.Open("postgres", connectURL)
	if err != nil {
		return "", err
	}
	defer db.Close()

	count := 0
	err = db.QueryRow(
		fmt.Sprintf("SELECT COUNT(*) FROM pg_catalog.pg_database WHERE datname = '%s' ;", databaseName)).
		Scan(&count)
	if err != nil {
		return "", err
	}
	if count == 0 {
		// not found for $databaseName, create it
		_, err = db.Exec(fmt.Sprintf("CREATE DATABASE %s", databaseName))
		if err != nil {
			return "", err
		}
	}

	// replace dbname in url
	// from: postgres://postgres:docker@192.168.99.100:9071/postgres?sslmode=disable
	// to: postgres://postgres:docker@192.168.99.100:9071/$databaseName?sslmode=disable
	u, err := url.Parse(connectURL)
	if err != nil {
		return "", err
	}
	u.Path = fmt.Sprintf("/%s", databaseName)
	return u.String(), nil
}
