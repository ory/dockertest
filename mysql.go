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
	// MySQLUsername must be passed as username when connecting to mysql
	MySQLUsername = "root"

	// MySQLPassword must be passed as password when connecting to mysql
	MySQLPassword = "root"
)

var Mysql5 = Specification{
	Image: "mysql:5",
	Waiter: RegexWaiter(
		"MySQL init process done. Ready for start up",
		"mysqld: ready for connections",
	),
	Services: SimpleServiceMap{
		"main": SimpleService(3306, fmt.Sprintf("%s:%s@tcp({{.}})/mysql", MySQLUsername, MySQLPassword)),
	},
	Env: Env{
		"MYSQL_ROOT_PASSWORD": MySQLPassword,
	},
}

var Mysql55 = Mysql5.WithVersion("5.5")
var Mysql56 = Mysql5.WithVersion("5.6")
var Mysql57 = Mysql5.WithVersion("5.7")

var MariaDB55 = Mysql5.WithImage("mariadb:5.5")
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
