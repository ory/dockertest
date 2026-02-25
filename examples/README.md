# dockertest examples

Integration test examples demonstrating the dockertest v4 API.

Run with Docker available:

```sh
go test ./...
```

Compile-check only (no Docker required):

```sh
go test -short ./...
```

## Examples

| File | Service | Features |
|------|---------|----------|
| `postgres_test.go` | PostgreSQL | `NewPoolT`, `RunT`, `Cleanup`, `Retry`, `GetHostPort` |
| `redis_test.go` | Redis | `go-redis/v9` client, SET/GET verification |
| `mysql_test.go` | MySQL | DDL + DML assertions, `go-sql-driver/mysql` |
| `mongodb_test.go` | MongoDB | `mongo-driver`, BSON insert/find |
| `cockroachdb_test.go` | CockroachDB | `WithCmd`, single-node insecure mode |
| `build_dockerfile_test.go` | PostgreSQL (custom image) | `BuildAndRunT`, `BuildOptions`, `testdata/Dockerfile` |
| `mountebank_test.go` | Mountebank | HTTP health check, `WithCmd`, `WithoutReuse` |
| `minio_test.go` | MinIO | HTTP health check, `WithEnv`, `minio-go/v7` |
| `cassandra_test.go` | Cassandra | `GetBoundIP`, `GetPort`, `gocql` auth |
| `multi_container_test.go` | PostgreSQL (x2) | `CreateNetworkT`, `ConnectToNetwork`, `GetIPInNetwork` |
