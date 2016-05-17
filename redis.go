package dockertest

var redisWaiter = RegexWaiter("The server is now ready to accept connections on port 6379")
var redisSvcMap = SimpleServiceMap{
	"main": SimpleService(6379, "{{.}}"),
}

var Redis3 = Specification{
	Image:    "redis:3",
	Waiter:   redisWaiter,
	Services: redisSvcMap,
}
var Redis30 = Redis3.WithVersion("3.0")
var Redis32 = Redis3.WithVersion("3.2")
