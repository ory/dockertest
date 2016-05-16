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

var Redis30 = Specification{
	Image:    "redis:3.0",
	Waiter:   redisWaiter,
	Services: redisSvcMap,
}

var Redis32 = Specification{
	Image:    "redis:3.2",
	Waiter:   redisWaiter,
	Services: redisSvcMap,
}
