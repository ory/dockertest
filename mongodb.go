package dockertest

var MongoDB3 = Specification{
	Image:  "mongo:3",
	Waiter: RegexWaiter("waiting for connections on port 27017"),
	Services: SimpleServiceMap{
		"main": SimpleService(27017, "{{.}}"),
	},
}
