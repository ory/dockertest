package dockertest

import (
	"database/sql"
	"fmt"

	mysql "github.com/go-sql-driver/mysql"
)

const (
	defaultMySQLDBName = "mysql"
)

var (
	// mySQLUsername must be passed as username when connecting to mysql
	mySQLUsername = "root"

	// mySQLPassword must be passed as password when connecting to mysql
	mySQLPassword = "root"
)

var MySQL5 = Specification{
	Image: "mysql:5",
	Waiter: RegexWaiter(
		"MySQL init process done. Ready for start up",
		"mysqld: ready for connections",
	),
	Services: SimpleServiceMap{
		"main": SimpleService(3306, fmt.Sprintf("%s:%s@tcp({{.}})/mysql", mySQLUsername, mySQLPassword)),
	},
	Env: Env{
		"MYSQL_ROOT_PASSWORD": mySQLPassword,
	},
}

var MySQL55 = MySQL5.WithVersion("5.5")
var MySQL56 = MySQL5.WithVersion("5.6")
var MySQL57 = MySQL5.WithVersion("5.7")

var MariaDB55 = MySQL5.WithImage("mariadb:5.5")
var MariaDB10 = MariaDB55.WithVersion("10")

// SetUpMySQLDatabase connects mysql container with given $connectURL and also creates a new database named $databaseName
// A modified url used to connect the created database will be returned
func SetUpMySQLDatabase(databaseName, connectURL string) (url string, err error) {
	if databaseName == defaultMySQLDBName {
		return connectURL, nil
	}

	db, err := sql.Open("mysql", connectURL)
	if err != nil {
		return "", err
	}
	defer db.Close()
	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", databaseName))
	if err != nil {
		return "", err
	}

	// parse dsn
	config, err := mysql.ParseDSN(connectURL)
	if err != nil {
		return "", err
	}
	config.DBName = databaseName // overwrite database name
	return config.FormatDSN(), nil
}
