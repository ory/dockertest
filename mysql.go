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

var mysqlWaiter = RegexWaiter(
	"MySQL init process done. Ready for start up",
	"mysqld: ready for connections",
)
var mysqlServiceMap = SimpleServiceMap{
	"main": SimpleService(3306, fmt.Sprintf("%s:%s@tcp({{.}})/mysql", MySQLUsername, MySQLPassword)),
}
var mysqlEnv = Env{
	"MYSQL_ROOT_PASSWORD": MySQLPassword,
}

var Mysql55 = Specification{
	Image:    "mysql:5.5",
	Waiter:   mysqlWaiter,
	Services: mysqlServiceMap,
	Env:      mysqlEnv,
}

var Mysql56 = Specification{
	Image:    "mysql:5.6",
	Waiter:   mysqlWaiter,
	Services: mysqlServiceMap,
	Env:      mysqlEnv,
}

var Mysql57 = Specification{
	Image:    "mysql:5.7",
	Waiter:   mysqlWaiter,
	Services: mysqlServiceMap,
	Env:      mysqlEnv,
}

var MariaDB55 = Specification{
	Image:    "mariadb:5.5",
	Waiter:   mysqlWaiter,
	Services: mysqlServiceMap,
	Env:      mysqlEnv,
}

var MariaDB100 = Specification{
	Image:    "mariadb:10.0",
	Waiter:   mysqlWaiter,
	Services: mysqlServiceMap,
	Env:      mysqlEnv,
}

var MariaDB101 = Specification{
	Image:    "mariadb:10.1",
	Waiter:   mysqlWaiter,
	Services: mysqlServiceMap,
	Env:      mysqlEnv,
}

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
